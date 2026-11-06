import json
import os
from pathlib import Path
import unittest
from unittest.mock import patch

from deploy_stack import commands, summary, main
from production_apply import Blocked
from staging_image import validate


class NormalDeploymentTests(unittest.TestCase):
    def test_apply_refreshes_to_match_the_preview_and_uses_the_same_full_plan(self):
        preview, apply = commands('gcpProd', Path('/private/plan.json'))
        self.assertIn('--refresh', preview)
        self.assertIn('--refresh', apply)
        self.assertNotIn('--refresh=false', apply)
        self.assertEqual(preview[preview.index('--save-plan')+1], apply[apply.index('--plan')+1])
        self.assertNotIn('--target', preview + apply)
        self.assertIn('--skip-preview', apply)

    def test_ordinary_application_change_is_reported_without_private_values(self):
        event = {'resourcePreEvent': {'metadata': {'op': 'update', 'urn': 'app',
                 'old': {'inputs': {'secret': 'do-not-print'}}, 'new': {'inputs': {'image': 'new'}}}}}
        result = summary('\n'.join(map(json.dumps, [event, {'summaryEvent': {'resourceChanges': {'update': 1}}}])))
        self.assertEqual(result['resources'], [{'operation':'update','urn':'app'}])
        self.assertNotIn('do-not-print', json.dumps(result))
        self.assertNotIn('new', json.dumps(result))

    def test_failed_or_incomplete_preview_cannot_proceed(self):
        for events in [[], [{'summaryEvent': {'resourceChanges': {}}}, {'diagnosticEvent': {'severity':'error'}}],
                       [{'summaryEvent': {'resourceChanges': {}, 'maybeCorrupt': True}}]]:
            with self.subTest(events=events), self.assertRaises(Blocked):
                summary('\n'.join(map(json.dumps,events)))

    def test_ci_apply_requires_main_before_any_command(self):
        with patch.dict(os.environ, {'GITHUB_ACTIONS':'true','GITHUB_REF':'refs/heads/feature'}), patch('deploy_stack.quiet') as command:
            with self.assertRaises(Blocked): main('prod','apply')
            command.assert_not_called()

    def test_build_digest_rejects_wrong_repository_mutable_tag_and_output_injection(self):
        image='ghcr.io/modelcontextprotocol/registry@sha256:'+'a'*64
        self.assertEqual(validate(image),image)
        for bad in ['ghcr.io/modelcontextprotocol/registry:main', image+'\nnext=bad', image.replace('/registry@','/other@'), image[:-1]]:
            with self.subTest(image=bad), self.assertRaises(ValueError): validate(bad)


class HelmPlanReferenceTests(unittest.TestCase):
    def plan(self):
        stack='gcpStaging';prefix='urn:pulumi:'+stack+'::mcp-registry-infra::'
        chart=prefix+'kubernetes:helm.sh/v3:Chart::ingress-nginx'
        name='ingress-nginx-admission-create';identity='ingress-nginx/'+name
        urn=prefix+'kubernetes:helm.sh/v3:Chart$kubernetes:batch/v1:Job::'+identity
        reference={'urn':urn,'id':''}
        goal={'type':'kubernetes:batch/v1:Job','parent':chart,'inputDiff':{'adds':{'metadata':{'name':name,'namespace':'ingress-nginx'},'spec':{'unchanged':True}}}}
        document={'version':1,'plan':{'resourcePlans':{
            chart:{'goal':{'outputDiff':{'updates':{'resources':{'batch/v1/Job::'+identity:reference}}}}},
            urn:{'steps':['create'],'goal':goal}}}}
        return document,reference,goal

    def test_only_empty_reference_is_constrained(self):
        import copy
        from deploy_stack import constrain_admission_job_ids
        doc,reference,goal=self.plan();original=copy.deepcopy(goal)
        self.assertEqual(constrain_admission_job_ids(doc,'gcpStaging'),1)
        self.assertEqual(reference['id'],'ingress-nginx/ingress-nginx-admission-create')
        self.assertEqual(goal,original)
        self.assertEqual(constrain_admission_job_ids(doc,'gcpStaging'),0)

    def test_unknown_identity_cannot_be_filled(self):
        from deploy_stack import constrain_admission_job_ids
        doc,reference,goal=self.plan();goal['inputDiff']['adds']['metadata']['name']='different'
        with self.assertRaises(Blocked): constrain_admission_job_ids(doc,'gcpStaging')
        self.assertEqual(reference['id'],'')


if __name__ == '__main__': unittest.main()
