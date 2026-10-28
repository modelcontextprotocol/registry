"""Summarize ingress access records without persisting URLs or client identifiers.

The collector supplies complete, bounded time slices and records pagination
coverage separately. This module does not turn a sample into a traffic census.
"""
import collections
import datetime
import hashlib
import math
import re
import secrets
import urllib.parse


ACCESS = re.compile(
    r'^\S+ - \S+ \[[^\]]+\] "(?P<method>\S+) (?P<uri>\S+) (?P<protocol>HTTP/[\d.]+)" '
    r'(?P<status>\d{3}) (?P<bytes>\d+) "[^"\n]*" "[^"\n]*" '
    r'(?P<request_bytes>\d+) (?P<duration>[\d.]+) '
)
SAFE_CACHE = {"HIT", "MISS", "STALE", "UPDATING", "EXPIRED", "BYPASS", "REVALIDATED"}


def parse_timestamp(value):
    # Python 3.9 accepts microseconds; Cloud Logging uses RFC3339 nanoseconds.
    value = re.sub(r'(\.\d{6})\d+', r'\1', value)
    result = datetime.datetime.fromisoformat(value.replace('Z', '+00:00'))
    if result.tzinfo is None:
        raise ValueError('timezone missing')
    return result


def percentile(values, fraction):
    return sorted(values)[max(0, math.ceil(len(values) * fraction) - 1)] if values else None


def size_band(value):
    for upper in (1024, 4096, 16384, 65536, 262144, 1048576):
        if value <= upper:
            return str(upper)
    return ">1048576"


def request_shape(uri):
    url = urllib.parse.urlsplit(uri)
    params = urllib.parse.parse_qs(url.query, keep_blank_values=True)
    version = "v0.1" if url.path.startswith('/v0.1/') else "v0" if url.path.startswith('/v0/') else "other"
    listing = url.path in ('/v0/servers', '/v0.1/servers')
    if listing:
        kind = "search" if params.get('search', [''])[0] else "updated" if params.get('updated_since', [''])[0] else "page"
    elif re.fullmatch(r'/v0(?:\.1)?/servers/.+/versions(?:/.+)?', url.path):
        kind = "detail"
    elif url.path in ('/v0/publish', '/v0.1/publish'):
        kind = "publish"
    else:
        kind = "other"
    limit = params.get('limit', ['default'])[0]
    # Bound values; never preserve an arbitrary parameter value as a label.
    if limit != 'default':
        limit = str(int(limit)) if limit.isdecimal() and 0 < int(limit) <= 100 else 'invalid'
    return kind, version, limit, bool(params.get('cursor', [''])[0]), params


class Profile:
    def __init__(self):
        self.salt = secrets.token_bytes(32)
        self.parsed = self.unparsed = 0
        self.groups = {}
        self.seconds = collections.Counter()
        self.shapes = collections.Counter()
        self.key_counts = collections.Counter()
        self.key_sets_by_second = collections.defaultdict(set)
        self.pages_with_cursor = 0
        self.search_lengths = collections.Counter()
        self.updated_ages = collections.Counter()
        self.page_filters = collections.Counter()
        self.page_class_keys = collections.defaultdict(collections.Counter)
        self.unsupported_page_requests = 0
        self.first = self.last = None

    def add(self, message, timestamp, pod):
        match = ACCESS.match(message)
        if not match:
            self.unparsed += 1
            return
        row = match.groupdict()
        if row['method'] not in ('GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'):
            row['method'] = 'other'
        if row['protocol'] not in ('HTTP/1.0', 'HTTP/1.1', 'HTTP/2', 'HTTP/2.0', 'HTTP/3', 'HTTP/3.0'):
            row['protocol'] = 'other'
        self.parsed += 1
        # NGINX background subrequests have zero request_length in this log
        # format. Preserve this population separately rather than inflate RPS.
        group = 'client' if int(row['request_bytes']) > 0 else 'zero_request_length'
        state = self.groups.setdefault(group, {
            'count': 0, 'cache': collections.Counter(), 'protocol': collections.Counter(),
            'kind': collections.Counter(), 'version': collections.Counter(),
            'status': collections.Counter(), 'pod': collections.Counter(),
            'response_bytes': [], 'duration_seconds': [], 'body_size_buckets': collections.Counter(),
        })
        kind, version, limit, cursor, params = request_shape(row['uri'])
        cache = re.search(r'cache_status="([^"]*)"', message)
        cache = cache.group(1) if cache and cache.group(1) in SAFE_CACHE else 'none'
        state['count'] += 1
        for field, value in [('cache', cache), ('protocol', row['protocol']), ('kind', kind),
                             ('version', version), ('status', row['status']), ('pod', pod)]:
            state[field][value] += 1
        state['response_bytes'].append(int(row['bytes']))
        state['duration_seconds'].append(float(row['duration']))
        state['body_size_buckets'][size_band(int(row['bytes']))] += 1
        self.first = min(self.first, timestamp) if self.first is not None else timestamp
        self.last = max(self.last, timestamp) if self.last is not None else timestamp
        if group != 'client':
            return
        self.seconds[int(timestamp)] += 1
        self.shapes['|'.join([row['method'], kind, version, limit, row['protocol'], 'cursor' if cursor else 'first'])] += 1
        if kind == 'page':
            key = hashlib.blake2b(row['uri'].encode(), key=self.salt).digest()
            self.key_counts[key] += 1
            self.key_sets_by_second[int(timestamp)].add(key)
            self.pages_with_cursor += int(cursor)
            version_filter = params.get('version', [''])[0]
            version_filter = 'none' if not version_filter else 'latest' if version_filter == 'latest' else 'exact'
            known = {'cursor', 'limit', 'version', 'search', 'updated_since', 'include_deleted'}
            extras = 'other_params' if set(params) - known else 'known_params_only'
            deleted = 'include_deleted' if params.get('include_deleted', [''])[0].lower() == 'true' else 'active_only'
            self.page_filters['|'.join([version, limit, version_filter, deleted, extras])] += 1
            effective_limit = '30' if limit == 'default' else limit
            if version in ('v0', 'v0.1') and effective_limit != 'invalid' and version_filter in ('none', 'latest') and deleted == 'active_only' and extras == 'known_params_only':
                # Normalize query ordering/default limit for the model; raw
                # URI diversity above remains the observed cache-key measure.
                canonical = dict(params)
                canonical['limit'] = [effective_limit]
                normalized = urllib.parse.urlsplit(row['uri']).path + '?' + urllib.parse.urlencode(sorted(canonical.items()), doseq=True)
                model_key = hashlib.blake2b(normalized.encode(), key=self.salt).digest()
                self.page_class_keys[(version, int(effective_limit), '' if version_filter == 'none' else 'latest')][model_key] += 1
            else:
                self.unsupported_page_requests += 1
        if kind == 'search':
            n = len(params['search'][0])
            self.search_lengths[str(n) if n <= 20 else '>20'] += 1
        if kind == 'updated':
            try:
                dt = parse_timestamp(params['updated_since'][0])
                age = timestamp - dt.timestamp()
                band = 'future' if age < 0 else '<1h' if age < 3600 else '<1d' if age < 86400 else '<7d' if age < 604800 else '>=7d'
            except (ValueError, OverflowError):
                band = 'invalid'
            self.updated_ages[band] += 1

    def result(self):
        groups = {}
        for name, state in self.groups.items():
            groups[name] = {k: dict(v) if isinstance(v, collections.Counter) else v
                            for k, v in state.items() if k not in ('response_bytes', 'duration_seconds')}
            for name2 in ('response_bytes', 'duration_seconds'):
                values = state[name2]
                groups[name][name2] = {'mean': sum(values) / len(values), 'p50': percentile(values, .5),
                                     'p95': percentile(values, .95), 'p99': percentile(values, .99), 'max': max(values)}
        counts = sorted(self.key_counts.values(), reverse=True)
        pages = sum(counts)
        return {
            'parsed': self.parsed, 'unparsed': self.unparsed, 'first_log_timestamp': self.first,
            'last_log_timestamp': self.last, 'populations': groups,
            'client_completions_per_second': dict(sorted(self.seconds.items())),
            'client_shapes': dict(self.shapes),
            'page_keys': {'distinct': len(counts), 'requests': pages, 'cursor_requests': self.pages_with_cursor,
                          'top_128_share': sum(counts[:128]) / pages if pages else None,
                          'singleton_keys': sum(n == 1 for n in counts),
                          'distinct_per_second': {str(t): len(s) for t, s in sorted(self.key_sets_by_second.items())}},
            'search_length_counts': dict(self.search_lengths), 'updated_age_counts': dict(self.updated_ages),
            'page_filter_counts': dict(self.page_filters),
            'page_workload': {
                'classes': [{'api_version': version, 'limit': limit, 'version_filter': filt,
                             'key_counts': sorted(counts.values(), reverse=True)}
                            for (version, limit, filt), counts in sorted(self.page_class_keys.items())],
                'excluded_page_requests': self.unsupported_page_requests,
                'limitations': ['Frequency model only; chronological crawl/client ordering is not replayed.',
                                'Query ordering and default limit normalized; original cache-key diversity is reported separately.',
                                'Searches, updates, details and publishing need their own measured workloads.'],
            },
            'limitations': ['Time slices and completion-time bins, not a full request arrival trace.',
                           'Zero request_length separated as a background-work proxy.',
                           'Body byte counts are logged wire sizes; compression can differ from generator decoded bytes.',
                           'No TCP connection IDs in this log format: connection reuse/lifetime remains unmeasured.',
                           'No URLs, search text, IPs, user agents, request IDs or key hashes are persisted.'],
        }
