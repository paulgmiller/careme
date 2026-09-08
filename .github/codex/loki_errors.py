"""Fetch a bounded, grouped error report; never persist the Loki credential."""
import base64
import collections
import datetime
import json
import os
import pathlib
import re
import urllib.parse
import urllib.request


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def summarize(streams, token):
    groups = collections.Counter()
    for stream in streams:
        labels = stream['stream']
        for entry in stream['values']:
            message = entry[1]
            error = labels.get('exception_message', '')
            # Drop identifiers and request attributes; retain diagnostic text only.
            text = json.dumps({'message': message, 'error': error,
                               'backend': labels.get('backend', ''),
                               'version': labels.get('service_version', '')})
            text = text.replace(token, '[REDACTED]') if token else text
            text = re.sub(r'(?i)(bearer\s+)[^\s"\\]+', r'\1[REDACTED]', text)
            text = re.sub(r'[\w.+-]+@[\w.-]+\.[A-Za-z]{2,}', '[EMAIL]', text)
            groups[text] += 1
    return [dict(json.loads(text), count=count) for text, count in groups.most_common()]


def main():
    token = os.environ['LOKI_TOKEN']
    if not token:
        raise ValueError('LOKI_TOKEN secret is empty')
    endpoint = os.environ['LOKI_URL'].rstrip('/')
    if urllib.parse.urlparse(endpoint).scheme != 'https':
        raise ValueError('LOKI_URL must use HTTPS')
    auth = base64.b64encode((os.environ['LOKI_USER'] + ':' + token).encode()).decode()
    end = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0)
    start = end - datetime.timedelta(hours=26)
    streams = []
    cursor = start
    opener = urllib.request.build_opener(NoRedirect())
    while cursor < end:
        next_cursor = min(cursor + datetime.timedelta(hours=1), end)
        params = urllib.parse.urlencode({
            'query': '{service_name="careme", deployment_environment_name="production"} | severity_number >= 17',
            'start': str(int(cursor.timestamp()) * 1_000_000_000),
            'end': str(int(next_cursor.timestamp()) * 1_000_000_000 - 1),
            'limit': '5000', 'direction': 'forward',
        })
        req = urllib.request.Request(endpoint + '/loki/api/v1/query_range?' + params,
                                     headers={'Authorization': 'Basic ' + auth})
        with opener.open(req, timeout=60) as response:
            result = json.load(response)
        if result.get('status') != 'success':
            raise ValueError('Loki query did not succeed')
        batch = result['data']['result']
        if sum(len(s['values']) for s in batch) >= 5000:
            raise ValueError('Hourly Loki result hit 5000 entries; narrow the query before retrying')
        streams.extend(batch)
        cursor = next_cursor
    report = {'start': start.isoformat(), 'end': end.isoformat(), 'errors': summarize(streams, token)}
    destination = pathlib.Path(os.environ['RUNNER_TEMP']) / 'loki-errors.json'
    destination.write_text(json.dumps(report, indent=2))
    destination.chmod(0o600)
    with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
        output.write(f"has_errors={'true' if report['errors'] else 'false'}\n")
    print(f"Found {sum(e['count'] for e in report['errors'])} errors in {len(report['errors'])} groups.")


if __name__ == '__main__':
    main()
