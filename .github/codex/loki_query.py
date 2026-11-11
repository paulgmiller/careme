"""Read Loki logs for a LogQL query chosen by the investigating agent."""
import argparse
import base64
import datetime
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def timestamp(value):
    parsed = datetime.datetime.fromisoformat(value.replace('Z', '+00:00'))
    if parsed.tzinfo is None:
        raise ValueError('timestamps must include a timezone, e.g. 2026-09-08T12:00:00Z')
    return parsed


def redact(value, token):
    if isinstance(value, str):
        value = value.replace(token, '[REDACTED]')
        value = re.sub(r'(?i)(bearer\s+)[^\s"\\]+', r'\1[REDACTED]', value)
        return re.sub(r'[\w.+-]+@[\w.-]+\.[A-Za-z]{2,}', '[EMAIL]', value)
    if isinstance(value, list):
        return [redact(item, token) for item in value]
    if isinstance(value, dict):
        return {key: redact(item, token) for key, item in value.items()}
    return value


def query_logs(query, start, end, limit):
    if start >= end:
        raise ValueError('start must be earlier than end')
    if not 1 <= limit <= 5000:
        raise ValueError('limit must be between 1 and 5000')
    token = os.environ.get('LOKI_TOKEN', '')
    user = os.environ.get('LOKI_USER', '')
    if not token or not user:
        raise ValueError('LOKI_TOKEN and LOKI_USER must be set')
    endpoint = os.environ.get('LOKI_URL', '').rstrip('/')
    parsed = urllib.parse.urlparse(endpoint)
    if parsed.scheme != 'https' or not parsed.hostname or parsed.username or parsed.query or parsed.fragment:
        raise ValueError('LOKI_URL must be an HTTPS endpoint without credentials, query, or fragment')
    params = urllib.parse.urlencode({
        'query': query, 'start': start.isoformat(), 'end': end.isoformat(),
        'limit': str(limit), 'direction': 'forward',
    })
    auth = base64.b64encode((user + ':' + token).encode()).decode()
    request = urllib.request.Request(endpoint + '/loki/api/v1/query_range?' + params,
                                     headers={'Authorization': 'Basic ' + auth})
    opener = urllib.request.build_opener(NoRedirect())
    with opener.open(request, timeout=60) as response:
        result = json.load(response)
    if result.get('status') != 'success':
        raise ValueError('Loki query did not succeed')
    data = result['data']
    if data.get('resultType') != 'streams':
        raise ValueError('Use a log query; this helper expects a streams result')
    count = sum(len(stream['values']) for stream in data['result'])
    # Keep timestamps, labels, and structured metadata for follow-up correlation.
    return redact({'start': start.isoformat(), 'end': end.isoformat(),
                   'query': query, 'count': count, 'limit_reached': count >= limit,
                   'streams': data['result']}, token)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--query', required=True, help='LogQL log query')
    parser.add_argument('--start', help='ISO timestamp with timezone; defaults to 24 hours before end')
    parser.add_argument('--end', help='ISO timestamp with timezone; defaults to now')
    parser.add_argument('--limit', type=int, default=500, help='Maximum log lines, 1–5000 (default 500)')
    args = parser.parse_args()
    try:
        end = timestamp(args.end) if args.end else datetime.datetime.now(datetime.timezone.utc)
        start = timestamp(args.start) if args.start else end - datetime.timedelta(hours=24)
        print(json.dumps(query_logs(args.query, start, end, args.limit), indent=2))
    except urllib.error.HTTPError as error:
        # Do not print response bodies or authentication headers.
        print(f'Loki HTTP {error.code}; check credentials, LogQL, and time range.', file=sys.stderr)
        return 1
    except (urllib.error.URLError, TimeoutError):
        print('Loki connection failed or timed out.', file=sys.stderr)
        return 1
    except (ValueError, KeyError) as error:
        print(f'Loki query failed: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
