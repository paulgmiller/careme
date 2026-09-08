import io
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import loki_errors


class LokiErrorsTest(unittest.TestCase):
    def test_groups_and_redacts(self):
        streams = [{'stream': {'exception_message': 'Bearer abc person@example.com',
                               'user_id': 'private'},
                    'values': [['1', 'secret'], ['2', 'secret']]}]
        report = loki_errors.summarize(streams, 'secret')
        self.assertEqual(report[0]['count'], 2)
        for value in ['secret', 'abc', 'person@example.com', 'private']:
            self.assertNotIn(value, json.dumps(report))

    def test_fetch_and_empty_results(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / 'output'
            env = {'LOKI_TOKEN': 'secret', 'LOKI_USER': '123',
                   'LOKI_URL': 'https://loki.example', 'RUNNER_TEMP': directory,
                   'GITHUB_OUTPUT': str(output)}
            with patch.dict(os.environ, env), patch('loki_errors.urllib.request.build_opener') as factory:
                factory.return_value.open.side_effect = lambda *a, **kw: io.BytesIO(
                    b'{"status":"success","data":{"result":[]}}')
                loki_errors.main()
                calls = factory.return_value.open.call_args_list
                self.assertEqual(len(calls), 26)
                self.assertIn('severity_number', calls[0].args[0].full_url)
                self.assertEqual(output.read_text(), 'has_errors=false\n')
                report = json.loads((Path(directory) / 'loki-errors.json').read_text())
                self.assertEqual(report['errors'], [])

    def test_truncation_is_an_error(self):
        with tempfile.TemporaryDirectory() as directory:
            env = {'LOKI_TOKEN': 'secret', 'LOKI_USER': '123',
                   'LOKI_URL': 'https://loki.example', 'RUNNER_TEMP': directory,
                   'GITHUB_OUTPUT': str(Path(directory) / 'output')}
            result = {'status': 'success', 'data': {'result': [
                {'stream': {}, 'values': [['1', 'error']] * 5000}]}}
            with patch.dict(os.environ, env), patch('loki_errors.urllib.request.build_opener') as factory:
                factory.return_value.open.return_value = io.BytesIO(json.dumps(result).encode())
                with self.assertRaisesRegex(ValueError, '5000'):
                    loki_errors.main()
                self.assertFalse((Path(directory) / 'output').exists())


if __name__ == '__main__':
    unittest.main()
