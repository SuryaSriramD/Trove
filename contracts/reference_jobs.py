"""Phase 0 job/pipe protocol reference. Durable production scheduler remains Go work."""
import json
import struct
from .reference_service import ContractError

TRANSITIONS={
 'queued':{'running','cancelled'},
 'running':{'succeeded','retry_wait','failed','cancel_requested','interrupted'},
 'retry_wait':{'running','cancelled'},
 'cancel_requested':{'cancelled','interrupted'},
 'interrupted':{'retry_wait','failed','cancelled'},
 'succeeded':set(),'failed':set(),'cancelled':set(),
}
JOB_PAYLOADS={'IMPORT_NOTION':'import','EXTRACT_DOCUMENT':'file','INDEX_CONTENT':'index','GENERATE_EMBEDDINGS':'embedding','TRANSCRIBE_AUDIO':'transcribe','SUMMARIZE_MEETING':'summarize','CREATE_THUMBNAIL':'file','BACKUP_WORKSPACE':'backup','MODEL_PACK_ACQUIRE':'model','INVALIDATE_MEMORY':'memory','AUTOMATION_RUN':'automation'}

def check_job_payload(kind,payload):
 if JOB_PAYLOADS.get(kind)!=payload['kind']:raise ContractError('INVALID_REQUEST')

def transition(db,job_id,target,*,lease=None,now=0):
 db.execute('BEGIN IMMEDIATE')
 try:
  row=db.execute('SELECT state,attempt,max_attempts,lease_token,lease_expires_at,next_attempt_at,revision FROM jobs WHERE job_id=?',(job_id,)).fetchone()
  if not row:raise ContractError('NOT_FOUND')
  state,attempt,maximum,old_lease,expiry,next_attempt,revision=row
  if revision>=9223372036854775807:raise ContractError('RESOURCE_EXHAUSTED')
  if target not in TRANSITIONS[state]:raise ContractError('CONFLICT')
  if state in ('running','cancel_requested') and (old_lease!=lease or (target!='interrupted' and int(expiry)<=now)):raise ContractError('CONFLICT')
  if target=='running':
   if state=='retry_wait' and (next_attempt is None or int(next_attempt)>now):raise ContractError('CONFLICT')
   if attempt>=maximum:raise ContractError('RESOURCE_EXHAUSTED')
   if not lease:raise ContractError('INVALID_REQUEST')
   attempt+=1; expiry=str(now+30)
  elif target=='cancel_requested':lease=old_lease
  else:lease=None;expiry=None
  next_attempt=str(now+min(60,2**max(0,attempt-1))) if target=='retry_wait' else None
  db.execute('UPDATE jobs SET state=?,attempt=?,lease_token=?,lease_expires_at=?,next_attempt_at=?,revision=revision+1 WHERE job_id=?',(target,attempt,lease,expiry,next_attempt,job_id))
  db.execute('COMMIT')
 except Exception:db.execute('ROLLBACK');raise

def recover_expired(db,now):
 for id,state,lease in list(db.execute("SELECT job_id,state,lease_token FROM jobs WHERE state IN ('running','cancel_requested') AND CAST(lease_expires_at AS INTEGER)<=?",(now,))):
  transition(db,id,'interrupted',lease=lease,now=now)
  if state=='cancel_requested':transition(db,id,'cancelled',now=now)
  else:
   attempt,maximum=db.execute('SELECT attempt,max_attempts FROM jobs WHERE job_id=?',(id,)).fetchone()
   transition(db,id,'retry_wait' if attempt<maximum else 'failed',now=now)

MAX_FRAME=1024*1024
class FrameDecoder:
 def __init__(self):self.buffer=bytearray();self.expected=None
 def feed(self,data):
  # A production read is at most 64 KiB; this reference rejects oversized read chunks.
  if len(data)>65536:raise ContractError('RESOURCE_EXHAUSTED')
  self.buffer.extend(data);results=[]
  while True:
   if self.expected is None:
    if len(self.buffer)<4:break
    self.expected=struct.unpack('>I',self.buffer[:4])[0];del self.buffer[:4]
    if not 0<self.expected<=MAX_FRAME:raise ContractError('RESOURCE_EXHAUSTED')
   if len(self.buffer)<self.expected:break
   raw=bytes(self.buffer[:self.expected]);del self.buffer[:self.expected];self.expected=None
   try:results.append(json.loads(raw,parse_constant=lambda _: (_ for _ in ()).throw(ValueError('nonfinite'))))
   except (ValueError,UnicodeError) as e:raise ContractError('INVALID_REQUEST') from e
  return results
 def eof(self):
  if self.buffer or self.expected is not None:raise ContractError('WORKER_FAILED')

def frame(value):
 raw=json.dumps(value,separators=(',',':'),allow_nan=False).encode()
 if len(raw)>MAX_FRAME:raise ContractError('RESOURCE_EXHAUSTED')
 return struct.pack('>I',len(raw))+raw
