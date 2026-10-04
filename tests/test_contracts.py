"""Cross-language schemas, example payloads and adversarial boundary checks."""
import copy
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import unittest
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource

ROOT=Path(__file__).resolve().parents[1]
SCHEMAS={p.stem.split('.')[0]:json.loads(p.read_text()) for p in (ROOT/'contracts').glob('*.schema.json') if not p.name.startswith('._')}
REGISTRY=Registry().with_resources([(v['$id'],Resource.from_contents(v)) for v in SCHEMAS.values()])
def validate(doc,definition,value):
 Draft202012Validator({'$ref':SCHEMAS[doc]['$id']+'#/$defs/'+definition},registry=REGISTRY,format_checker=FormatChecker()).validate(value)
class ContractTests(unittest.TestCase):
 def test_all_schema_definitions_are_valid(self):
  for doc in SCHEMAS.values():Draft202012Validator.check_schema(doc)
 def test_protocol_examples_validate(self):
  for item in json.loads((ROOT/'contracts/examples/protocol-manifest.json').read_text()):
   with self.subTest(example=item['path']):validate(item['schema'],item['definition'],json.loads((ROOT/'contracts/examples'/item['path']).read_text()))
 def test_generator_has_no_drift(self):
  paths=[ROOT/'contracts/types_generated.go',*(ROOT/'contracts/generated'/f'{m}.ts' for m in SCHEMAS)]
  before={p:p.read_bytes() for p in paths}
  subprocess.run([sys.executable,str(ROOT/'scripts/generate_types.py')],check=True)
  self.assertEqual(before,{p:p.read_bytes() for p in paths})
 def test_protocol_authoring_has_no_drift(self):
  paths=[ROOT/'contracts/service.schema.json',ROOT/'contracts/jobs.schema.json']
  before={p:p.read_bytes() for p in paths}
  subprocess.run([sys.executable,str(ROOT/'scripts/build_protocol_schemas.py')],check=True)
  self.assertEqual(before,{p:p.read_bytes() for p in paths})
 def test_wire_rejects_actor_injection_unsafe_revision_unknown_command_and_empty_batch(self):
  base=json.loads((ROOT/'contracts/examples/service-batch.json').read_text())
  cases=[]
  r=copy.deepcopy(base);r['actor']={'kind':'user'};cases.append(r)
  r=copy.deepcopy(base);r['mutation']['expectedRevisions'][0]['revision']=9007199254740993;cases.append(r)
  r=copy.deepcopy(base);r['commands'][0]['kind']='sql.execute';cases.append(r)
  r=copy.deepcopy(base);r['commands']=[];cases.append(r)
  r=copy.deepcopy(base);r['contractVersion']=2;cases.append(r)
  for r in cases:
   with self.subTest(request=r),self.assertRaises(Exception):validate('service','BatchRequest',r)
 def test_worker_limits_and_citation_identity_are_structural(self):
  r=json.loads((ROOT/'contracts/examples/worker-transcribe.json').read_text());r['limits']['memoryBytes']=2**50
  with self.assertRaises(Exception):validate('jobs','WorkerRequest',r)
  loc={'pageId':'0199a000-0000-7000-8000-000000000001','sourceRevision':'1','transcript':{'meetingId':'0199a000-0000-7000-8000-000000000002','segmentId':'0199a000-0000-7000-8000-000000000003','startMs':0,'endMs':100}}
  with self.assertRaises(Exception):validate('domain','Location',loc)
 def test_tasks_and_memory_commands_share_domain_vocabulary(self):
  defs=SCHEMAS['service']['$defs'];domain=SCHEMAS['domain']['$defs']
  self.assertEqual(defs['ProposeMemory']['properties']['memoryKind']['enum'],domain['Memory']['properties']['kind']['enum'])
  self.assertIn('containerKind',defs['CreateContainer']['required'])
  self.assertEqual(defs['CreateContainer']['properties']['containerKind']['enum'],domain['Container']['properties']['kind']['enum'])
  task={'kind':'task.create','recordId':'0199a000-0000-7000-8000-000000000001','pageId':'0199a000-0000-7000-8000-000000000002','title':'Task','collectionId':'0199a000-0000-7000-8000-000000000003'}
  with self.assertRaises(Exception):validate('service','CreateTask',task)

 def test_title_value_is_projection_and_edits_use_page_update(self):
  with self.assertRaises(Exception):validate('service','ValueInput',{'type':'title','schemaVersion':1,'value':{'pageId':'0199a000-0000-7000-8000-000000000001'}})
  validate('service','UpdatePage',{'kind':'page.update','pageId':'0199a000-0000-7000-8000-000000000001','title':'Renamed'})
