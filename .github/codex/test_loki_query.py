import datetime
import io
import json
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlparse

import loki_query


class LokiQueryTest(unittest.TestCase):
    def setUp(self):
        self.start = loki_query.timestamp('2026-09-07T12:00:00Z')
        self.end = self.start + datetime.timedelta(minutes=5)
        self.env = {'LOKI_TOKEN': 'secret', 'LOKI_USER': '123',
                    'LOKI_URL': 'https://loki.example'}

    def test_query_keeps_correlation_and_reports_limit(self):
        streams = [{'stream': {'exception_message': 'Bearer abc person@example.com',
                               'trace_id': 'trace-123', 'request_id': 'request-456'},
                    'values': [['123', 'secret', {'session_id': 'session-789'}]]}]
        response = {'status': 'success', 'data': {'resultType': 'streams', 'result': streams}}
        with patch.dict('os.environ', self.env), patch('loki_query.urllib.request.build_opener') as factory:
            factory.return_value.open.return_value = io.BytesIO(json.dumps(response).encode())
            query = '{service_name="careme"} | trace_id="trace-123"'
            report = loki_query.query_logs(query, self.start, self.end, 1)
            request = factory.return_value.open.call_args.args[0]
            params = parse_qs(urlparse(request.full_url).query)
            self.assertEqual(params['query'], [query])
            self.assertEqual(params['start'], [self.start.isoformat()])
            self.assertEqual(params['end'], [self.end.isoformat()])
            self.assertEqual(request.get_header('Authorization'), 'Basic MTIzOnNlY3JldA==')
            self.assertEqual(factory.return_value.open.call_args.kwargs['timeout'], 60)
            self.assertTrue(report['limit_reached'])
            self.assertEqual(report['count'], 1)
            for value in ['trace-123', 'request-456', 'session-789']:
                self.assertIn(value, json.dumps(report))
            for value in ['secret', 'abc', 'person@example.com']:
                self.assertNotIn(value, json.dumps(report))

    def test_empty_query_is_success(self):
        with patch.dict('os.environ', self.env), patch('loki_query.urllib.request.build_opener') as factory:
            factory.return_value.open.return_value = io.BytesIO(
                b'{"status":"success","data":{"resultType":"streams","result":[]}}')
            report = loki_query.query_logs('{service_name="careme"}', self.start, self.end, 500)
            self.assertEqual(report['streams'], [])
            self.assertFalse(report['limit_reached'])

    def test_invalid_range_and_limit(self):
        for start, end, limit in [(self.end, self.start, 500),
                                  (self.start, self.end, 0), (self.start, self.end, 5001)]:
            with self.subTest(limit=limit), self.assertRaises(ValueError):
                loki_query.query_logs('{}', start, end, limit)
        with self.assertRaises(ValueError):
            loki_query.timestamp('2026-09-08T12:00:00')

    def test_no_redirect(self):
        self.assertIsNone(loki_query.NoRedirect().redirect_request(None, None, 302, '', {}, 'https://other.example'))

    def test_query_error_is_not_an_empty_result(self):
        with patch.dict('os.environ', self.env), patch('loki_query.urllib.request.build_opener') as factory:
            factory.return_value.open.return_value = io.BytesIO(b'{"status":"error"}')
            with self.assertRaisesRegex(ValueError, 'did not succeed'):
                loki_query.query_logs('{}', self.start, self.end, 500)


if __name__ == '__main__':
    unittest.main()
