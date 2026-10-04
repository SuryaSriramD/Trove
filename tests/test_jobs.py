import sqlite3
import struct
import tempfile
import unittest
from pathlib import Path
from contracts.reference_jobs import transition,recover_expired,frame,FrameDecoder,check_job_payload
from contracts.reference_service import WorkspaceReference,ContractError

class JobTests(unittest.TestCase):
 def setUp(self):
  self.k=WorkspaceReference('0199a000-0000-7000-8000-000000000001');self.db=self.k.db
  self.db.execute("INSERT INTO jobs(job_id,workspace_id,kind,payload_version,payload_json,state,revision,idempotency_key,attempt,max_attempts,created_at,updated_at) VALUES ('j','0199a000-0000-7000-8000-000000000001','INDEX_CONTENT',1,'{}','queued',1,'key',0,2,'now','now')")
 def state(self):return self.db.execute("SELECT state,attempt FROM jobs WHERE job_id='j'").fetchone()
 def test_claim_is_exclusive_and_old_worker_is_fenced(self):
  transition(self.db,'j','running',lease='first',now=0)
  with self.assertRaisesRegex(ContractError,'CONFLICT'):transition(self.db,'j','running',lease='second')
  recover_expired(self.db,31);self.assertEqual(tuple(self.state()),('retry_wait',1))
  transition(self.db,'j','running',lease='second',now=32)
  with self.assertRaisesRegex(ContractError,'CONFLICT'):transition(self.db,'j','succeeded',lease='first',now=33)
  transition(self.db,'j','succeeded',lease='second',now=33);self.assertEqual(tuple(self.state()),('succeeded',2))
 def test_cancel_crash_does_not_restart_and_terminal_is_final(self):
  transition(self.db,'j','running',lease='first');transition(self.db,'j','cancel_requested',lease='first')
  recover_expired(self.db,31);self.assertEqual(tuple(self.state()),('cancelled',1))
  with self.assertRaisesRegex(ContractError,'CONFLICT'):transition(self.db,'j','running',lease='second')
 def test_retry_budget_is_bounded(self):
  transition(self.db,'j','running',lease='first');recover_expired(self.db,31)
  transition(self.db,'j','running',lease='second',now=32);recover_expired(self.db,63)
  self.assertEqual(tuple(self.state()),('failed',2))
 def test_jobs_survive_connection_restart(self):
  with tempfile.TemporaryDirectory() as d:
   p=Path(d)/'copy.db';copy=sqlite3.connect(p);self.db.backup(copy);copy.close()
   restored=sqlite3.connect(p,isolation_level=None)
   transition(restored,'j','running',lease='restored')
   self.assertEqual(restored.execute("SELECT state FROM jobs WHERE job_id='j'").fetchone()[0],'running');restored.close()
 def test_frames_fragment_coalesce_and_reject_oversized_truncated_or_nonfinite(self):
  wire=frame({'kind':'progress','sequence':1})+frame({'kind':'cancelled','sequence':2});decoder=FrameDecoder();out=[]
  for byte in wire:out.extend(decoder.feed(bytes([byte])))
  decoder.eof();self.assertEqual([x['sequence'] for x in out],[1,2])
  with self.assertRaisesRegex(ContractError,'RESOURCE_EXHAUSTED'):FrameDecoder().feed(struct.pack('>I',1024*1024+1))
  d=FrameDecoder();d.feed(wire[:10])
  with self.assertRaisesRegex(ContractError,'WORKER_FAILED'):d.eof()
  with self.assertRaisesRegex(ContractError,'INVALID_REQUEST'):FrameDecoder().feed(struct.pack('>I',3)+b'NaN')
 def test_job_kind_and_payload_cannot_drift(self):
  check_job_payload('TRANSCRIBE_AUDIO',{'kind':'transcribe'})
  with self.assertRaisesRegex(ContractError,'INVALID_REQUEST'):check_job_payload('TRANSCRIBE_AUDIO',{'kind':'backup'})

 def test_retry_wait_respects_due_time_and_revision_overflow(self):
  transition(self.db,'j','running',lease='first')
  transition(self.db,'j','retry_wait',lease='first',now=1)
  with self.assertRaisesRegex(ContractError,'CONFLICT'):transition(self.db,'j','running',lease='second',now=1)
  transition(self.db,'j','running',lease='second',now=2)
  self.db.execute("UPDATE jobs SET revision=9223372036854775807 WHERE job_id='j'")
  with self.assertRaisesRegex(ContractError,'RESOURCE_EXHAUSTED'):transition(self.db,'j','succeeded',lease='second',now=3)
  with self.assertRaises(sqlite3.IntegrityError):self.db.execute("UPDATE jobs SET revision=revision+1 WHERE job_id='j'")
