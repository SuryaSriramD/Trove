"""Executable Phase 0 transaction oracle, not the production Go kernel.
Only page create/update is executed here to isolate cross-cutting service guarantees.
Every product command must satisfy the same guarantees in Go integration tests.
"""
import hashlib
import json
import sqlite3
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]
class ContractError(Exception):
    pass

def canonical_hash(request,proposal=False):
    # Reference fixtures use integer/string JSON only. Production uses RFC8785.
    semantic={k:request[k] for k in ('workspaceId','mutation','commands')}
    semantic['mutation']=dict(semantic['mutation'])
    semantic['mutation']['expectedRevisions']=sorted(semantic['mutation']['expectedRevisions'],key=lambda x:(x['target']['entityType'],x['target']['entityId']))
    if proposal:semantic['mutation'].pop('approvalId',None)
    return hashlib.sha256(json.dumps(semantic,sort_keys=True,separators=(',',':'),ensure_ascii=False).encode()).hexdigest()

class WorkspaceReference:
    def __init__(self,workspace_id,path=':memory:'):
        self.db=sqlite3.connect(path,isolation_level=None)
        self.db.row_factory=sqlite3.Row
        self.db.executescript((ROOT/'schemas/workspace.sql').read_text())
        self.db.executescript((ROOT/'schemas/services.sql').read_text())
        self.db.execute('INSERT INTO workspaces VALUES (?,1)',(workspace_id,))
        self.workspace_id=workspace_id
        self.allowed_pages=set()
        self.policy_revision=1
        self.approvals={}
        self.grant_id=None
    def approve(self,approval_id,request,actor,expires_at=100):
        self.approvals[approval_id]={'hash':canonical_hash(request,proposal=True),'actor':actor,'expires':expires_at,'policy':self.policy_revision,'used':False}
    def page(self,id):
        row=self.db.execute('SELECT title,revision FROM pages WHERE id=?',(id,)).fetchone()
        return dict(row) if row else None
    def batch(self,request,*,actor='owner',actor_kind='user',origin='user',now=0,cancel_before=False,fail_after=None):
        valid_origins={'user':{'user','restore'},'local_ai':{'ai'},'external_client':{'external'},'importer':{'import'},'job':{'system'},'automation':{'automation'},'migration':{'migration'}}
        if origin not in valid_origins.get(actor_kind,set()):raise ContractError('SCOPE_DENIED')
        if request['workspaceId']!=self.workspace_id or request['mutation']['scopeGrantId']!=self.grant_id:raise ContractError('SCOPE_DENIED')
        commands=request['commands']; meta=request['mutation']; digest=canonical_hash(request)
        touched={c['pageId'] for c in commands}
        if not touched <= self.allowed_pages:raise ContractError('SCOPE_DENIED')
        self.db.execute('BEGIN IMMEDIATE')
        try:
            receipt=self.db.execute('SELECT * FROM idempotency_receipts WHERE workspace_id=? AND actor_id=? AND idempotency_key=?',(self.workspace_id,actor,meta['idempotencyKey'])).fetchone()
            if receipt:
                if receipt['command_hash']!=digest:raise ContractError('IDEMPOTENCY_CONFLICT')
                response=json.loads(receipt['response_json']);response['replayed']=True
                self.db.execute('COMMIT');return response
            approval=None
            if actor_kind!='user':
                approval=self.approvals.get(meta.get('approvalId'))
                if not approval:raise ContractError('APPROVAL_REQUIRED')
                if approval['expires']<=now or approval['used']:raise ContractError('APPROVAL_EXPIRED')
                if approval['hash']!=canonical_hash(request,proposal=True) or approval['actor']!=actor or approval['policy']!=self.policy_revision:raise ContractError('APPROVAL_REQUIRED')
            expected={x['target']['entityId']:x['revision'] for x in meta['expectedRevisions'] if x['target']['entityType']=='page'}
            if len(expected)!=len(meta['expectedRevisions']) or set(expected)!=touched:raise ContractError('INVALID_REQUEST')
            before={id:self.page(id) for id in touched}
            for id in touched:
                actual=before[id]['revision'] if before[id] else '0'
                if expected[id]!=actual:raise ContractError('CONFLICT')
            if cancel_before:raise ContractError('CANCELLED')
            for i,c in enumerate(commands):
                if c['kind']=='page.create':
                    if before[c['pageId']] is not None:raise ContractError('CONFLICT')
                    self.db.execute('INSERT INTO pages(id,workspace_id,parent_page_id,title,revision,created_at,updated_at) VALUES (?,?,?,?,?,?,?)',(c['pageId'],self.workspace_id,None,c['title'],'1','2026-09-30T00:00:00Z','2026-09-30T00:00:00Z'))
                elif c['kind']=='page.update':
                    if before[c['pageId']] is None:raise ContractError('NOT_FOUND')
                    if 'title' not in c:raise ContractError('INVALID_REQUEST')
                    self.db.execute('UPDATE pages SET title=? WHERE id=?',(c['title'],c['pageId']))
                else:raise ContractError('INVALID_REQUEST')
                if fail_after==i:raise ContractError('INTEGRITY_ERROR')
            for id in touched:
                revision=1 if before[id] is None else int(before[id]['revision'])+1
                if revision>9223372036854775807:raise ContractError('RESOURCE_EXHAUSTED')
                self.db.execute('UPDATE pages SET revision=? WHERE id=?',(str(revision),id))
            after={id:self.page(id) for id in touched}
            tx=request['requestId'] # Fixture-only deterministic transaction ID.
            self.db.execute('INSERT INTO mutation_history VALUES (?,?,?,?,?,?,?,?,?,?,?,?)',(tx,tx,tx,actor,origin,'{}',self.policy_revision,meta.get('approvalId'),digest,json.dumps(before),json.dumps(after),'2026-09-30T00:00:00Z'))
            cur=self.db.execute('INSERT INTO committed_events(event_id,transaction_id,payload_json) VALUES (?,?,?)',(tx,tx,json.dumps(after)))
            result={'transactionId':tx,'eventCursor':str(cur.lastrowid),'replayed':False}
            self.db.execute('INSERT INTO idempotency_receipts VALUES (?,?,?,?,?,?)',(self.workspace_id,actor,meta['idempotencyKey'],digest,tx,json.dumps(result)))
            self.db.execute('COMMIT')
            if approval:approval['used']=True
            return result
        except Exception:
            self.db.execute('ROLLBACK');raise
