import copy
import unittest
from contracts.reference_service import WorkspaceReference,ContractError

def uid(n):return f'0199a000-0000-7000-8000-{n:012d}'
def request(n=3,title='First',kind='page.create',revision='0',page=2):
 return {'contractVersion':1,'requestId':uid(n),'workspaceId':uid(1),'mutation':{'idempotencyKey':f'action-{n}','scopeGrantId':uid(99),'expectedRevisions':[{'target':{'entityType':'page','entityId':uid(page)},'revision':revision}]},'commands':[{'kind':kind,'pageId':uid(page),'title':title}]}
class ServiceTests(unittest.TestCase):
 def setUp(self):
  self.kernel=WorkspaceReference(uid(1));self.kernel.grant_id=uid(99);self.kernel.allowed_pages={uid(2),uid(4)}
 def test_atomic_batch_rolls_back_content_history_and_events(self):
  r=request();r['commands'].append({'kind':'page.create','pageId':uid(4),'title':'Second'});r['mutation']['expectedRevisions'].append({'target':{'entityType':'page','entityId':uid(4)},'revision':'0'})
  with self.assertRaisesRegex(ContractError,'INTEGRITY_ERROR'):self.kernel.batch(r,fail_after=1)
  self.assertIsNone(self.kernel.page(uid(2)));self.assertIsNone(self.kernel.page(uid(4)))
  for table in ['mutation_history','committed_events','idempotency_receipts']:self.assertEqual(self.kernel.db.execute('SELECT count(*) FROM '+table).fetchone()[0],0)
 def test_retry_after_lost_ack_replays_one_commit(self):
  r=request();a=self.kernel.batch(r);r['requestId']=uid(10);b=self.kernel.batch(r)
  self.assertEqual(a['transactionId'],b['transactionId']);self.assertTrue(b['replayed'])
  self.assertEqual(self.kernel.db.execute('SELECT count(*) FROM committed_events').fetchone()[0],1)
  r['commands'][0]['title']='Changed'
  with self.assertRaisesRegex(ContractError,'IDEMPOTENCY_CONFLICT'):self.kernel.batch(r)
 def test_stale_revision_and_missing_guard_never_overwrite(self):
  self.kernel.batch(request())
  with self.assertRaisesRegex(ContractError,'CONFLICT'):self.kernel.batch(request(5,'stale','page.update','0'))
  r=request(6,'unguarded','page.update','1');r['mutation']['expectedRevisions']=[]
  with self.assertRaisesRegex(ContractError,'INVALID_REQUEST'):self.kernel.batch(r)
  self.assertEqual(self.kernel.page(uid(2)),{'title':'First','revision':'1'})
 def test_ai_approval_binds_exact_proposal_actor_policy_and_expiry(self):
  r=request();r['mutation']['approvalId']=uid(98)
  with self.assertRaisesRegex(ContractError,'APPROVAL_REQUIRED'):self.kernel.batch(r,actor='ai',actor_kind='local_ai',origin='ai')
  self.kernel.approve(uid(98),r,'ai')
  with self.assertRaisesRegex(ContractError,'APPROVAL_REQUIRED'):self.kernel.batch(r,actor='other',actor_kind='local_ai',origin='ai')
  with self.assertRaisesRegex(ContractError,'APPROVAL_EXPIRED'):self.kernel.batch(r,actor='ai',actor_kind='local_ai',origin='ai',now=101)
  self.kernel.policy_revision+=1
  with self.assertRaisesRegex(ContractError,'APPROVAL_REQUIRED'):self.kernel.batch(r,actor='ai',actor_kind='local_ai',origin='ai')
  self.kernel.approve(uid(98),r,'ai');self.kernel.batch(r,actor='ai',actor_kind='local_ai',origin='ai')
  self.assertTrue(self.kernel.approvals[uid(98)]['used'])
 def test_scope_revoked_and_origin_spoof_rejected(self):
  self.kernel.batch(request());self.kernel.allowed_pages=set()
  with self.assertRaisesRegex(ContractError,'SCOPE_DENIED'):self.kernel.batch(request())
  self.kernel.allowed_pages={uid(2)}
  with self.assertRaisesRegex(ContractError,'SCOPE_DENIED'):self.kernel.batch(request(),actor_kind='external_client',origin='user')
 def test_cancellation_before_commit_and_revision_once_per_batch(self):
  with self.assertRaisesRegex(ContractError,'CANCELLED'):self.kernel.batch(request(),cancel_before=True)
  self.assertIsNone(self.kernel.page(uid(2)));self.kernel.batch(request())
  r=request(9,'a','page.update','1');r['commands'].append({'kind':'page.update','pageId':uid(2),'title':'b'})
  self.kernel.batch(r);self.assertEqual(self.kernel.page(uid(2)),{'title':'b','revision':'2'})
