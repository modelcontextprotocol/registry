#!/usr/bin/env python3
"""Bounded public-Gateway recovery tests; always restore cordons and policy patches."""
import argparse
import copy
import json
import os
import re
import signal
import uuid
import subprocess
import time
from pathlib import Path
from datetime import datetime
from measure import K, NS, get, gateway_target
from public_probe import PublicProbe


def run(*args, timeout=240, input=None):
    return subprocess.run(K + list(args), input=input, capture_output=True, text=True, check=True, timeout=timeout)


def wait_rollout(namespace, name):
    run('-n', namespace, 'rollout', 'status', 'deployment/' + name, '--timeout=360s', timeout=370)


def apply_policy(current, spec, manager):
    # Preserve the existing SSA owner and use resourceVersion as an optimistic
    # concurrency guard. No force override is allowed in qualification runs.
    metadata = {k: current['metadata'][k] for k in ['name', 'namespace', 'resourceVersion', 'labels', 'annotations', 'ownerReferences'] if k in current['metadata']}
    manifest = {'apiVersion': current['apiVersion'], 'kind': current['kind'], 'metadata': metadata, 'spec': spec}
    run('apply', '--server-side', '--field-manager=' + manager, '-f', '-', input=json.dumps(manifest))


def generator_timestamp(value):
    # Python 3.9 accepts microseconds, while Go emits RFC3339 nanoseconds.
    value = re.sub(r'(\.\d{6})\d+', r'\1', value)
    return datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp()


def wait_measurement(load, timeout, grace=10):
    """Terminate the isolated process group on timeout, then reap its leader."""
    try:
        return load.wait(timeout=timeout), False
    except subprocess.TimeoutExpired:
        try:
            os.killpg(load.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        # Keep the leader unreaped until descendants have been killed: its PID
        # cannot be reused as an unrelated process-group ID in this interval.
        time.sleep(grace)
        try:
            os.killpg(load.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        return load.wait(timeout=grace), True


def stop_remote_load(epoch):
    # Disconnecting kubectl exec alone does not prove the remote command exited.
    # Match both the exact binary and this run's random epoch before signalling.
    script = r"""import os,sys,signal,time
from pathlib import Path
def owned(pid):
 try:
  args=(Path('/proc')/str(pid)/'cmdline').read_bytes().split(b'\0')
  return args[0]==b'/work/loadgen' and any(args[i]==b'-epoch' and args[i+1]==sys.argv[1].encode() for i in range(len(args)-1))
 except (FileNotFoundError, ProcessLookupError): return False
pids=[int(p.name) for p in Path('/proc').iterdir() if p.name.isdigit() and owned(int(p.name))]
for pid in pids:
 try: os.kill(pid,signal.SIGTERM)
 except ProcessLookupError: pass
if pids: time.sleep(1)
for pid in pids:
 if owned(pid):
  try: os.kill(pid,signal.SIGKILL)
  except ProcessLookupError: pass
deadline=time.monotonic()+3
while any(owned(pid) for pid in pids):
 if time.monotonic()>=deadline: raise RuntimeError('Remote load did not exit')
 time.sleep(.05)
"""
    run('-n', NS, 'exec', 'load-generator', '--', 'python3', '-c', script, epoch, timeout=30)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--scenario', required=True, choices=['proxy', 'controller', 'rollout', 'node'])
    parser.add_argument('--rate', type=int, default=500)
    parser.add_argument('--seconds', type=int, help='Defaults: rollout 600, node 300, other scenarios 180')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    if args.seconds is None:
        args.seconds = {'rollout': 600, 'node': 300}.get(args.scenario, 180)
    assert os.environ.get('REGISTRY_GATEWAY_TARGET') == 'public'
    assert 0 < args.rate <= 1500 and 150 <= args.seconds <= 600
    args.output.mkdir(parents=True, exist_ok=True)
    (args.output / 'operation.json').write_text(json.dumps({'passed': False, 'scenario': args.scenario, 'stage': 'preflight'}) + '\n')
    edge_ns, edge_name = gateway_target()
    assert get('get', 'ingress', '-A')['items'] == [], 'Retire legacy Ingress objects before qualification'
    assert len(get('get', 'gateways', '-A')['items']) == 1, 'Remove the duplicate canary first'
    nodes = get('get', 'nodes')['items']
    node_names = sorted(n['metadata']['name'] for n in nodes)
    assert len(node_names) == 4
    gateway_nodes = [n for n in nodes if n['metadata']['labels'].get('registry-role') == 'gateway-canary']
    assert len(gateway_nodes) == 2 and not any(n['spec'].get('unschedulable') for n in gateway_nodes)
    selector = 'gateway.networking.k8s.io/gateway-name=registry-edge'
    proxy_pods = get('-n', edge_ns, 'get', 'pods', '-l', selector)['items']
    assert len({p['spec']['nodeName'] for p in proxy_pods if not p['metadata'].get('deletionTimestamp')}) == 2
    report = {'scenario': args.scenario, 'nodes_before': node_names, 'started': time.time()}
    cordoned = None
    original_patches = None
    modified_patches = None
    policy_uid = None
    policy_before = None
    policy_manager = None
    modified_spec = None
    failure = None
    public = PublicProbe(args.output / 'public-probes.json')
    public.start()
    epoch = 'public-' + args.scenario + '-' + uuid.uuid4().hex
    load_log = (args.output / 'load.log').open('w')
    load = subprocess.Popen(['python3', 'gateway-canary/measure.py', '--arm', 'ngf', '--rate', str(args.rate),
                             '--seconds', str(args.seconds), '--workers', '256', '--epoch', epoch,
                             '--output', str(args.output / 'load')], stdout=load_log, stderr=subprocess.STDOUT, start_new_session=True)
    try:
        time.sleep(30)
        assert load.poll() is None, 'Load finished before disruption'
        report['action_started'] = time.time()
        if args.scenario in ['proxy', 'controller']:
            namespace, name = (edge_ns, edge_name) if args.scenario == 'proxy' else (NS, 'registry-gateway-canary')
            deployment = get('-n', namespace, 'get', 'deployment', name)
            match = ','.join(k + '=' + v for k, v in deployment['spec']['selector']['matchLabels'].items())
            pod = next(p for p in get('-n', namespace, 'get', 'pods', '-l', match)['items'] if not p['metadata'].get('deletionTimestamp'))
            report['deleted_pod'] = pod['metadata']['name']
            run('-n', namespace, 'delete', 'pod', pod['metadata']['name'], '--wait=true', '--timeout=90s')
            wait_rollout(namespace, name)
        elif args.scenario == 'rollout':
            policy = get('-n', edge_ns, 'get', 'nginxproxy', 'registry-edge', '--show-managed-fields=true')
            policy_before = copy.deepcopy(policy)
            managers = {m['manager'] for m in policy['metadata']['managedFields'] if m['manager'].startswith('pulumi-kubernetes-') and m['operation'] == 'Apply' and 'f:spec' in m.get('fieldsV1', {})}
            assert len(managers) == 1, 'Expected one Pulumi owner of the Gateway policy'
            policy_manager = managers.pop()
            (args.output / 'policy-before.json').write_text(json.dumps(policy_before, indent=2) + '\n')
            policy_uid = policy['metadata']['uid']
            original_patches = policy['spec']['kubernetes']['deployment']['patches']
            modified_patches = copy.deepcopy(original_patches)
            value = str(int(time.time()))
            modified_patches[0]['value']['spec']['template'].setdefault('metadata', {}).setdefault('annotations', {})['registry.modelcontextprotocol.io/qualification'] = value
            modified_spec = copy.deepcopy(policy['spec'])
            modified_spec['kubernetes']['deployment']['patches'] = modified_patches
            apply_policy(policy, modified_spec, policy_manager)
            deadline = time.monotonic() + 60
            while get('-n', edge_ns, 'get', 'deployment', edge_name)['spec']['template']['metadata'].get('annotations', {}).get('registry.modelcontextprotocol.io/qualification') != value:
                assert time.monotonic() < deadline, 'Controller did not apply rollout policy'
                time.sleep(2)
            wait_rollout(edge_ns, edge_name)
        else:
            cordoned = gateway_nodes[0]['metadata']['name']
            report['drained_node'] = cordoned
            run('cordon', cordoned)
            run('drain', cordoned, '--ignore-daemonsets', '--delete-emptydir-data', '--timeout=360s', timeout=370)
            report['drained_at'] = time.time()
            deadline = time.monotonic() + 90
            while True:
                ready = [p for p in get('-n', edge_ns, 'get', 'pods', '-l', selector)['items']
                         if not p['metadata'].get('deletionTimestamp') and any(c['type'] == 'Ready' and c['status'] == 'True' for c in p.get('status', {}).get('conditions', []))]
                if ready and all(p['spec']['nodeName'] != cordoned for p in ready):
                    break
                assert time.monotonic() < deadline, 'No ready proxy on surviving node'
                time.sleep(2)
            assert load.poll() is None
            time.sleep(60)
            assert load.poll() is None, 'Load did not cover a minute of one-node operation'
            report['one_node_load_seconds'] = time.time() - report['drained_at']
        assert load.poll() is None, 'Load ended before the disruption recovered'
        report['action_ready'] = time.time()
    except Exception as error:
        failure = repr(error)
    finally:
        cleanup_errors = []
        if cordoned:
            report['one_node_ended'] = time.time()
            try:
                run('uncordon', cordoned)
                wait_rollout(edge_ns, edge_name)
                ready = [p for p in get('-n', edge_ns, 'get', 'pods', '-l', selector)['items'] if not p['metadata'].get('deletionTimestamp')]
                if len({p['spec']['nodeName'] for p in ready}) != 2:
                    # Kubernetes does not rebalance existing replicas after uncordon.
                    # Replace one gracefully so the normal spread rule can place it.
                    run('-n', edge_ns, 'delete', 'pod', ready[0]['metadata']['name'], '--wait=true', '--timeout=90s')
                    wait_rollout(edge_ns, edge_name)
                restored = get('-n', edge_ns, 'get', 'pods', '-l', selector)['items']
                assert len({p['spec']['nodeName'] for p in restored if not p['metadata'].get('deletionTimestamp')}) == 2, 'Gateway placement not restored'
            except Exception as error:
                cleanup_errors.append('uncordon: ' + repr(error))
        if modified_patches is not None:
            try:
                current = get('-n', edge_ns, 'get', 'nginxproxy', 'registry-edge')
                assert current['metadata']['uid'] == policy_uid and current['spec'] == modified_spec, 'Concurrent Gateway policy change; refuse restoration'
                apply_policy(current, policy_before['spec'], policy_manager)
                report['pulumi_policy_ownership_preserved'] = True
                deadline = time.monotonic() + 60
                while 'registry.modelcontextprotocol.io/qualification' in get('-n', edge_ns, 'get', 'deployment', edge_name)['spec']['template']['metadata'].get('annotations', {}):
                    assert time.monotonic() < deadline, 'Controller did not restore rollout policy'
                    time.sleep(2)
                wait_rollout(edge_ns, edge_name)
            except Exception as error:
                cleanup_errors.append('restore policy: ' + repr(error))
        report['cleanup_finished'] = time.time()
        report['cleanup_errors'] = cleanup_errors
        report['passed'] = False
        report['failure'] = failure or 'Waiting for measurement completion'
        (args.output / 'operation.json').write_text(json.dumps(report, indent=2) + '\n')
        try:
            report['measurement_exit'], timed_out = wait_measurement(load, args.seconds + 100)
            if timed_out:
                failure = failure or 'Measurement timed out; local process group terminated'
                stop_remote_load(epoch)
                report['remote_load_stopped'] = True
        except Exception as error:
            failure = failure or 'Measurement cleanup failed'
            cleanup_errors.append('measurement: ' + repr(error))
        finally:
            load_log.close()
            report['failure'] = failure
            (args.output / 'operation.json').write_text(json.dumps(report, indent=2) + '\n')
            report['public'] = public.finish(120 if args.scenario == 'node' else 0)
    report['nodes_after'] = sorted(n['metadata']['name'] for n in get('get', 'nodes')['items'])
    report['finished'] = time.time()
    report['passed'] = False
    report['failure'] = failure or 'Qualification checks not completed'
    (args.output / 'operation.json').write_text(json.dumps(report, indent=2) + '\n')
    # The measurement driver remains alive while scraping/draining metrics.
    # Qualify disruption overlap using the generator's own scheduling interval.
    measurement = json.loads((args.output / 'load' / 'result.json').read_text())
    client = measurement['client']
    active_start = generator_timestamp(client['started_at'])
    active_end = min(active_start + client['seconds'], generator_timestamp(client['finished_at']))
    report['generator_active_interval'] = [active_start, active_end]
    report['action_covered_by_load'] = active_start <= report.get('action_started', 0) < report.get('action_ready', float('inf')) <= active_end
    if not report['action_covered_by_load']:
        failure = failure or 'Generator traffic did not cover the disruption and recovery'
    if args.scenario == 'node':
        overlap = max(0, min(active_end, report.get('one_node_ended', 0)) - max(active_start, report.get('drained_at', float('inf'))))
        report['one_node_active_load_seconds'] = overlap
        if overlap < 60:
            failure = failure or 'Less than 60 seconds of actual one-node traffic'
    if args.scenario == 'rollout' and report['cleanup_finished'] > active_end:
        failure = failure or 'Generator traffic ended before the restoration rollout completed'
    report['failure'] = failure
    report['passed'] = failure is None and report['public']['passed'] and not report['cleanup_errors'] and report.get('measurement_exit') == 0 and report['nodes_before'] == report['nodes_after']
    (args.output / 'operation.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))
    assert report['passed'], 'Recovery qualification failed; inspect operation.json and load/result.json'


if __name__ == '__main__':
    main()
