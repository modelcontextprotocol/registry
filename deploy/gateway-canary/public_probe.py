"""Public HTTPS recovery observations for the staging disruption checks."""
import concurrent.futures
import json
import subprocess
import threading
import time

URLS = [
    'https://staging.registry.modelcontextprotocol.io/v0/health',
    'https://staging.registry.modelcontextprotocol.io/v0.1/servers?limit=1',
    'https://grafana.staging.registry.modelcontextprotocol.io/api/health',
]


class PublicProbe:
    def __init__(self, output):
        self.output = output
        self.rows = []
        self.stop = threading.Event()
        self.thread = threading.Thread(target=self.collect, daemon=True)

    @staticmethod
    def check(url):
        result = subprocess.run(['curl', '--silent', '--show-error', '--max-time', '8',
                                 '-o', '/dev/null', '-w', '%{http_code}', url],
                                capture_output=True, text=True, timeout=10)
        return {'url': url, 'ok': result.returncode == 0 and result.stdout == '200',
                'status': result.stdout, 'exit': result.returncode}

    def collect(self):
        with concurrent.futures.ThreadPoolExecutor(max_workers=len(URLS)) as pool:
            while not self.stop.is_set():
                started = time.time()
                try:
                    checks = list(pool.map(self.check, URLS))
                except Exception as error:
                    checks = [{'url': url, 'ok': False, 'error': type(error).__name__} for url in URLS]
                self.rows.append({'at': started, 'checks': checks})
                self.stop.wait(max(0, 2 - (time.time() - started)))

    def start(self):
        self.thread.start()

    def finish(self, allowed_recovery_seconds=0):
        self.stop.set()
        self.thread.join(timeout=15)
        assert not self.thread.is_alive(), 'Public probe did not stop'
        self.output.write_text(json.dumps(self.rows, indent=2) + '\n')
        failures = 0
        longest = 0
        recovered = bool(self.rows)
        for url in URLS:
            last_good = None
            outage = None
            for row in self.rows:
                check = next(c for c in row['checks'] if c['url'] == url)
                if check['ok']:
                    if outage is not None:
                        longest = max(longest, row['at'] - outage)
                        outage = None
                    last_good = row['at']
                else:
                    failures += 1
                    if outage is None:
                        outage = last_good if last_good is not None else row['at']
            recovered = recovered and outage is None and last_good is not None
        return {'probe_rounds': len(self.rows), 'failed_checks': failures,
                'maximum_recovery_seconds': longest, 'recovered': recovered,
                'allowed_recovery_seconds': allowed_recovery_seconds,
                'passed': recovered and (failures == 0 or (allowed_recovery_seconds > 0 and longest <= allowed_recovery_seconds))}
