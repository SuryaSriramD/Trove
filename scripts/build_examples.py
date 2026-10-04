import json
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
EX=ROOT/'contracts/examples'
def uid(n):return f'0199a000-0000-7000-8000-{n:012d}'
header={'contractVersion':1,'requestId':uid(3),'workspaceId':uid(1)}
location={'pageId':uid(2),'sourceRevision':'7','blockId':uid(4),'transcript':{'meetingId':uid(5),'segmentId':uid(6),'startMs':1200,'endMs':4800}}
context={'actor':{'kind':'user','actorId':uid(8)},'origin':'user','scope':{'kind':'workspace','workspaceId':uid(1)},'scopeGrantId':uid(9),'policyRevision':'1'}
changes=[{'target':{'entityType':'page','entityId':uid(2)},'before':'0','after':'1','deleted':False}]
examples={
 'service-batch':('service','BatchRequest',{**header,'mutation':{'idempotencyKey':'capture-2026-09-30-1','scopeGrantId':uid(9),'expectedRevisions':[{'target':{'entityType':'page','entityId':uid(2)},'revision':'0'}]},'commands':[{'kind':'page.create','pageId':uid(2),'title':'Meeting notes'}],'cancellationId':uid(10)}),
 'service-batch-result':('service','BatchResponse',{'ok':True,'result':{'requestId':uid(3),'transactionId':uid(11),'eventCursor':'1','revisions':changes,'replayed':False}}),
 'service-conflict':('service','BatchResponse',{'ok':False,'error':{'code':'CONFLICT','message':'Page changed since proposal','retryable':False,'requestId':uid(3),'details':{'currentRevisions':[{'target':{'entityType':'page','entityId':uid(2)},'revision':'8'}]}}}),
 'service-event':('service','CommittedChangeEvent',{'contractVersion':1,'eventId':uid(12),'workspaceId':uid(1),'transactionId':uid(11),'cursor':'1','committedAt':'2026-09-30T10:00:00Z','context':context,'revisions':changes,'mutationHistoryId':uid(13),'idempotencyKey':'capture-2026-09-30-1'}),
 'service-search':('service','SearchResponse',{'ok':True,'result':{'hits':[{'location':location,'excerpt':'Ship the migration report.','contentHash':'a'*64,'score':0.9,'sourceState':'current'}],'snapshotCursor':'7','indexGeneration':uid(14),'partial':False}}),
 'service-stale-citation':('service','ResolveCitationResponse',{'ok':True,'result':{'location':location,'state':'stale','currentRevision':'8','historicalSnapshotRef':uid(15)}}),
 'job-transcribe':('jobs','Job',{'jobId':uid(16),'workspaceId':uid(1),'kind':'TRANSCRIBE_AUDIO','payloadVersion':1,'payload':{'kind':'transcribe','meetingId':uid(5),'fileId':uid(17),'modelPackId':uid(18)},'state':'queued','revision':'1','idempotencyKey':'transcribe-file-hash-v1','attempt':0,'maxAttempts':3,'createdAt':'2026-09-30T10:00:00Z','updatedAt':'2026-09-30T10:00:00Z'}),
 'worker-transcribe':('jobs','WorkerRequest',{'protocolVersion':1,'requestId':uid(19),'jobId':uid(16),'leaseToken':uid(20),'cancellationId':uid(21),'operation':'transcribe','modelPackId':uid(18),'limits':{'threads':4,'memoryBytes':2147483648,'timeoutMs':3600000,'maxOutputBytes':16777216,'maxTokens':4096},'inputs':[{'handleId':uid(22),'kind':'audio','sha256':'b'*64,'sizeBytes':1920000}]}),
 'worker-result':('jobs','WorkerOutput',{'kind':'result','requestId':uid(19),'leaseToken':uid(20),'sequence':4,'resultHandle':{'handleId':uid(23),'kind':'result','sha256':'c'*64,'sizeBytes':256}}),
 'worker-transcript':('jobs','WorkerResult',{'kind':'transcript','meetingId':uid(5),'segments':[{'segmentId':uid(6),'blockId':uid(4),'startMs':1200,'endMs':4800,'text':'Ship the migration report.'}]}),
 'worker-cancel':('jobs','WorkerCancel',{'protocolVersion':1,'requestId':uid(19),'cancellationId':uid(21),'kind':'cancel'}),
 'job-progress':('jobs','JobProgressEvent',{'contractVersion':1,'jobId':uid(16),'jobRevision':'2','attempt':1,'state':'running','progress':{'sequence':1,'completed':3,'total':10,'unit':'segments'}}),
 'service-import-report':('service','GetImportReportResponse',{'ok':True,'result':{'runId':uid(24),'jobId':uid(25),'state':'partial','sourceCompleteness':'unknown','discoveredCount':2,'accountedCount':2,'items':[{'sourceLocator':'Notes.md','outcome':'preserved','reason':'Matched stable source identity'},{'sourceLocator':'Tabs.html','outcome':'unsupported_but_retained','reason':'P1 tabs retained as readable fallback','retainedFileId':uid(26)}],'committedBatchIds':[uid(27)],'unresolvedSourceIds':[],'sourceOmissions':['Export does not supply complete comment history']}}),
}
# Cross-workstream examples reference the complete domain workspace fixture.
domain=json.loads((EX/'domain-workspace.json').read_text())
task=domain['taskFacets'][0]
record=next(r for r in domain['records'] if r['id']==task['recordId'])
status=next(p for p in domain['properties'] if p['id']==task['statusPropertyId'])
done=status['config']['doneOptionId']
guards=[{'target':{'entityType':'record','entityId':record['id']},'revision':record['revision']},{'target':{'entityType':'page','entityId':record['pageId']},'revision':record['revision']}]
examples['service-task-toggle']=('service','BatchRequest',{**header,'mutation':{'idempotencyKey':'toggle-task-1','scopeGrantId':uid(9),'expectedRevisions':guards},'commands':[{'kind':'task.setStatus','recordId':record['id'],'statusOptionId':done}]})
resolution=json.loads((EX/'domain-import-resolution.json').read_text())
examples['service-import-apply']=('service','ApplyImportPlan',{'kind':'import.applyPlan','planId':uid(28),'planHash':'d'*64,'resolutions':[resolution]})
reimport_plan=json.loads((EX/'domain-reimport-plan.json').read_text())
examples['service-import-preview']=('service','PreviewImportResult',{'planId':uid(28),'planHash':'d'*64,'report':examples['service-import-report'][2]['result'],'reimportPlans':[reimport_plan],'requiredRevisions':[{'target':{'entityType':'page','entityId':next(m['localId'] for m in domain['importMappings'] if m['id']==reimport_plan['mappingId'])},'revision':reimport_plan['expectedLocalRevision']}]})
context_memory=json.loads((EX/'domain-memory-context.json').read_text())
examples['service-memory-proposal']=('service','ProposeMemory',{'kind':'memory.propose','memoryId':context_memory['id'],'pageId':context_memory['pageId'],'scope':context_memory['scope'],'memoryKind':'project_context','pageRef':context_memory['pageRef'],'sources':context_memory['sources']})
# P2 fixtures let UI and service owners compile against the same envelopes.
examples['service-p2-display']=('service','SetPageDisplay',{'kind':'page.display.set','pageId':uid(2),'fullWidth':True,'presentation':False})
examples['service-p2-database']=('service','ConfigureP2Database',{'kind':'database.p2Configure','collectionId':uid(30),'config':{'chart':{'groupPropertyId':None,'valuePropertyId':None,'aggregation':'count','visualization':'bar'},'form':{'enabled':True,'requiredPropertyIds':[]},'buttons':[]}})
examples['service-p2-form']=('service','SubmitLocalForm',{'kind':'database.formSubmit','collectionId':uid(30),'recordId':uid(31),'pageId':uid(32),'title':'Local submission','values':[]})
examples['service-p2-review']=('service','AcceptPersonalReview',{'kind':'review.accept','pageId':uid(2),'runId':uid(33),'blockId':uid(34),'approved':True})
examples['service-p2-graph']=('service','GraphQueryRequest',{**header,'pageId':uid(2),'limit':50,'includeSemantic':True})
examples['service-p2-ocr']=('service','OCRRunRequest',{**header,'fileId':uid(35)})
examples['service-p2-review-activate']=('service','ActivatePersonalReview',{'kind':'review.activate','pageId':uid(2),'planBlockId':uid(34),'enabled':True})
manifest=[]
for filename,(schema,definition,payload) in examples.items():
 (EX/(filename+'.json')).write_text(json.dumps(payload,indent=2)+'\n')
 manifest.append({'path':filename+'.json','schema':schema,'definition':definition})
(EX/'protocol-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
