"""Read bounded production Cloud Logging slices and save only aggregate profiles."""
import argparse
import datetime
import json
import pathlib
import subprocess
import urllib.request

from profile_logs import Profile, parse_timestamp


def collect(start, seconds, token, fetch=None, max_records=200000, project='mcp-registry-prod'):
    if not 1 <= seconds <= 300 or not 1 <= max_records <= 200000:
        raise ValueError('duration must be 1–300 seconds and record cap 1–200000')
    if project not in ('mcp-registry-prod', 'mcp-registry-staging'):
        raise ValueError('unsupported registry project')
    at = parse_timestamp(start)
    end = (at + datetime.timedelta(seconds=seconds)).isoformat()
    body = {
        'resourceNames': ['projects/' + project],
        'filter': f'resource.type="k8s_container" AND resource.labels.namespace_name="ingress-nginx" AND timestamp>="{at.isoformat()}" AND timestamp<"{end}"',
        'orderBy': 'timestamp asc', 'pageSize': min(10000, max_records),
    }
    if fetch is None:
        def fetch(query):
            request = urllib.request.Request(
                'https://logging.googleapis.com/v2/entries:list', data=json.dumps(query).encode(),
                headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'})
            with urllib.request.urlopen(request, timeout=45) as response:
                return json.load(response)
    profile = Profile()
    count = pages = 0
    complete, error = False, None
    try:
        while count < max_records:
            body['pageSize'] = min(10000, max_records-count)
            data = fetch(body)
            rows = data.get('entries', [])
            count += len(rows)
            pages += 1
            for row in rows:
                profile.add(row.get('textPayload', row.get('jsonPayload', {}).get('message', '')),
                            parse_timestamp(row['timestamp']).timestamp(),
                            row.get('resource', {}).get('labels', {}).get('pod_name', 'unknown'))
            if not data.get('nextPageToken'):
                complete = True
                break
            body['pageToken'] = data['nextPageToken']
            if pages >= 1000:
                raise RuntimeError('pagination bound exceeded')
    except Exception as exc:  # Preserve partial coverage; never print raw log data.
        error = type(exc).__name__
    output = {'start': at.isoformat(), 'end': end, 'seconds': seconds, 'entries': count,
              'pages': pages, 'query_complete': complete, 'record_cap': max_records,
              'error_type': error, **profile.result()}
    for group in output['populations'].values():
        group['records_per_second'] = group['count']/seconds if complete else None
    return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--start', required=True, help='RFC3339 timestamp with timezone')
    parser.add_argument('--seconds', type=int, default=60)
    parser.add_argument('--out', type=pathlib.Path, required=True)
    parser.add_argument('--project', choices=['mcp-registry-prod', 'mcp-registry-staging'],
                        default='mcp-registry-prod')
    args = parser.parse_args()
    token = subprocess.check_output(['gcloud', 'auth', 'print-access-token'], text=True, timeout=30).strip()
    result = collect(args.start, args.seconds, token, project=args.project)
    args.out.write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps({k: result[k] for k in ['query_complete', 'entries', 'parsed', 'unparsed', 'error_type']}))
    if not result['query_complete']:
        raise SystemExit(1)


if __name__ == '__main__':
    main()
