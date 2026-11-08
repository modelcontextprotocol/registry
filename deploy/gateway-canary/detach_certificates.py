#!/usr/bin/env python3
"""Detach staging public Certificates after adoption and shim removal, before retirement.

Uses the staging Pulumi passphrase from the environment; never prints stack state
or Secret values. UID/owner tests refuse concurrent changes. Safe to rerun after
retirement when the certificates already have no owners.
"""
import argparse,json,os,subprocess
from pathlib import Path
K=['kubectl','--context=gke_mcp-registry-staging_us-central1-b_mcp-registry-staging','--request-timeout=30s']
def run(*a):return subprocess.run(K+list(a),capture_output=True,text=True,check=True,timeout=60)
def get(*a):return json.loads(run(*a,'-o','json').stdout)
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--output',type=Path,required=True)
args=parser.parse_args()
env=dict(os.environ,PULUMI_BACKEND_URL='gs://mcp-registry-staging-pulumi-state')
r=subprocess.run(['pulumi','stack','export','--stack','gcpStaging','--non-interactive'],capture_output=True,text=True,check=True,timeout=120,env=env)
resources=json.loads(r.stdout)['deployment']['resources'];del r
report=[]
for ns,name,ingress in [('default','mcp-registry-staging-tls','mcp-registry'),('monitoring','grafana-tls','grafana-ingress')]:
 resource=next(x for x in resources if x['type']=='kubernetes:cert-manager.io/v1:Certificate' and x.get('id')==ns+'/'+name)
 assert resource.get('protect') is True and resource['urn'].startswith('urn:pulumi:gcpStaging::')
 assert resource['inputs']['metadata'].get('ownerReferences')==[]
 cert=get('-n',ns,'get','certificate',name); uid=cert['metadata']['uid'];owners=cert['metadata'].get('ownerReferences',[])
 before=run('-n',ns,'get','secret',name,'-o','jsonpath={.data.tls\\.crt}').stdout
 if owners:
  old=get('-n',ns,'get','ingress',ingress)
  assert 'cert-manager.io/cluster-issuer' not in old['metadata'].get('annotations',{})
  assert 'cert-manager.io/issuer' not in old['metadata'].get('annotations',{})
  assert len(owners)==1 and owners[0]['kind']=='Ingress' and owners[0]['uid']==old['metadata']['uid'] and owners[0]['name']==ingress
  patch=[{'op':'test','path':'/metadata/uid','value':uid},{'op':'test','path':'/metadata/ownerReferences','value':owners},{'op':'remove','path':'/metadata/ownerReferences'}]
  run('-n',ns,'patch','certificate',name,'--type=json','-p',json.dumps(patch))
 cert=get('-n',ns,'get','certificate',name)
 assert cert['metadata']['uid']==uid and not cert['metadata'].get('ownerReferences')
 assert run('-n',ns,'get','secret',name,'-o','jsonpath={.data.tls\\.crt}').stdout==before
 report.append({'namespace':ns,'certificate':name,'pulumi_protected':True,'owner_detached':True,'certificate_unchanged':True})
args.output.write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,indent=2))
