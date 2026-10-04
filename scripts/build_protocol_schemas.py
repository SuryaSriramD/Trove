"""Authoring source for service and worker JSON Schemas. Run after contract edits."""
import json
from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]
D = lambda name: {'$ref': f'domain.schema.json#/$defs/{name}'}
R = lambda name: {'$ref': f'#/$defs/{name}'}
S = {'type':'string'}
B = {'type':'boolean'}
N = {'type':'integer','minimum':0,'maximum':9007199254740991}
def enum(*v): return {'type':'string','enum':list(v)}
def arr(item, limit=1000): return {'type':'array','items':item,'maxItems':limit}
def obj(props, required=None): return {'type':'object','properties':props,'required':list(props) if required is None else required,'additionalProperties':False}
def tagged(tag, props, required=None):
    props = {'kind':{'const':tag}, **props}
    return obj(props, ['kind', *([key for key in props if key != 'kind'] if required is None else required)])
def schema(name,defs,root): return {'$schema':'https://json-schema.org/draft/2020-12/schema','$id':f'https://trove.local/contracts/{name}.schema.json','title':name,'$ref':f'#/$defs/{root}','$defs':defs}
def save(name,value): (ROOT/'contracts'/f'{name}.schema.json').write_text(json.dumps(value,indent=2)+'\n')
# The JSON value boundary is deliberately restricted to registry-validated data only.
json_value={'type':['object','array','string','number','integer','boolean','null']}
error_codes=['INVALID_REQUEST','UNSUPPORTED_VERSION','NOT_FOUND','CONFLICT','SCOPE_DENIED','APPROVAL_REQUIRED','APPROVAL_EXPIRED','IDEMPOTENCY_CONFLICT','CANCELLED','ALREADY_COMMITTED','RESOURCE_EXHAUSTED','WORKSPACE_LOCKED','UNSUPPORTED_SCHEMA','IMPORT_CONFLICT','INTEGRITY_ERROR','WORKER_FAILED','INTERNAL']
defs={
'EntityRef':obj({'entityType':enum('page','block','collection','property','record','view','container','file','meeting','transcript_segment','memory','project','import_plan','job'),'entityId':D('Id')}),
'ExpectedRevision':obj({'target':R('EntityRef'),'revision':{'oneOf':[D('Revision'),{'const':'0'}]}}),
'Error':obj({'code':enum(*error_codes),'message':S,'retryable':B,'requestId':D('Id'),'details':obj({'path':S,'currentRevisions':arr(R('ExpectedRevision')),'requiredApprovalHash':S,'retryAfterMs':N,'jobId':D('Id')},[])},['code','message','retryable','requestId']),
'Actor':obj({'kind':enum('user','local_ai','external_client','importer','job','automation','migration'),'actorId':D('Id'),'runId':D('Id')},['kind','actorId']),
'ExecutionContext':obj({'actor':R('Actor'),'origin':enum('user','ai','external','import','restore','automation','system','migration'),'scope':D('Scope'),'scopeGrantId':D('Id'),'policyRevision':D('Revision'),'approvalId':D('Id'),'causationId':D('Id')},['actor','origin','scope','scopeGrantId','policyRevision']),
'Approval':obj({'approvalId':D('Id'),'workspaceId':D('Id'),'actorId':D('Id'),'scopeGrantId':D('Id'),'policyRevision':D('Revision'),'commandHash':{'type':'string','pattern':'^[0-9a-f]{64}$'},'expectedRevisions':arr(R('ExpectedRevision')),'expiresAt':D('Timestamp'),'state':enum('pending','approved','consumed','revoked','expired'),'approvedBy':D('Id')},['approvalId','workspaceId','actorId','scopeGrantId','policyRevision','commandHash','expectedRevisions','expiresAt','state']),
'MutationMeta':obj({'idempotencyKey':{'type':'string','minLength':1,'maxLength':128},'scopeGrantId':D('Id'),'approvalId':D('Id'),'expectedRevisions':arr(R('ExpectedRevision'),1000)},['idempotencyKey','scopeGrantId','expectedRevisions']),
'ValueInput':obj({'type':{'type':'string','not':{'const':'title'}},'schemaVersion':{'type':'integer','minimum':1},'value':json_value}),
'RichTextInput':D('RichText'),
'BlockInput':obj({'blockId':D('Id'),'pageId':D('Id'),'parentBlockId':D('Id'),'type':S,'schemaVersion':{'type':'integer','minimum':1},'data':json_value,'afterBlockId':D('Id')},['blockId','pageId','type','schemaVersion','data']),
}
commands={
'CreatePage':tagged('page.create',{'pageId':D('Id'),'title':S,'parentPageId':D('Id')},['pageId','title']),
'UpdatePage':tagged('page.update',{'pageId':D('Id'),'title':S,'archived':B,'icon':{'oneOf':[{'type':'string','maxLength':128},{'type':'null'}]},'coverFileId':{'oneOf':[D('Id'),{'type':'null'}]}},['pageId']),
'MovePage':tagged('page.move',{'pageId':D('Id'),'parentPageId':{'oneOf':[D('Id'),{'type':'null'}]}}),
'TrashPage':tagged('page.trash',{'pageId':D('Id'),'includeDescendants':B}),
'InsertBlock':tagged('block.insert',{'block':R('BlockInput')}),
'UpdateBlock':tagged('block.update',{'blockId':D('Id'),'type':S,'schemaVersion':{'type':'integer','minimum':1},'data':json_value}),
'DeleteBlock':tagged('block.delete',{'blockId':D('Id'),'includeDescendants':B}),
'MoveBlock':tagged('block.move',{'blockId':D('Id'),'pageId':D('Id'),'parentBlockId':{'oneOf':[D('Id'),{'type':'null'}]},'afterBlockId':D('Id')},['blockId','pageId','parentBlockId']),
'ReorderBlocks':tagged('block.reorder',{'pageId':D('Id'),'parentBlockId':{'oneOf':[D('Id'),{'type':'null'}]},'orderedBlockIds':arr(D('Id'))}),
'CreateCollection':tagged('collection.create',{'collectionId':D('Id'),'owningPageId':D('Id'),'titlePropertyId':D('Id'),'title':S}),
'AddProperty':tagged('collection.addProperty',{'propertyId':D('Id'),'collectionId':D('Id'),'name':S,'type':S,'schemaVersion':{'type':'integer','minimum':1},'config':json_value}),
'CreateRecord':tagged('record.create',{'recordId':D('Id'),'collectionId':D('Id'),'pageId':D('Id'),'title':S}),
'SetFormula':tagged('property.setFormula',{'propertyId':D('Id'),'expression':{'type':'string','maxLength':8192},'evaluatorVersion':{'const':2}}),
'SetValue':tagged('record.setValue',{'recordId':D('Id'),'propertyId':D('Id'),'value':R('ValueInput')}),
'ClearValue':tagged('record.clearValue',{'recordId':D('Id'),'propertyId':D('Id')}),
'SaveView':tagged('view.save',{'viewId':D('Id'),'collectionId':D('Id'),'owningPageId':D('Id'),'containerId':D('Id'),'type':S,'schemaVersion':{'type':'integer','minimum':1},'config':json_value,'query':D('ViewQuery'),'name':S},['viewId','collectionId','owningPageId','type','schemaVersion','config','query','name']),
'SetProjectScope':tagged('project.setScope',{'projectId':D('Id'),'memberPageIds':arr(D('Id')),'pinnedPageIds':arr(D('Id')),'excludedPageIds':arr(D('Id')),'inheritAncestorPreferences':B,'inheritWorkspacePreferences':B}),
'CreateContainer':tagged('container.create',{'containerId':D('Id'),'owningPageId':D('Id'),'label':S,'containerKind':enum('database','project'),'parentProjectId':{'oneOf':[D('Id'),{'type':'null'}]}}),
'LinkCollection':tagged('container.linkCollection',{'containerId':D('Id'),'collectionId':D('Id')}),
'CreateTask':tagged('task.create',{'recordId':D('Id'),'pageId':D('Id'),'title':S,'collectionId':D('Id'),'statusPropertyId':D('Id')},['recordId','pageId','title']),
'EnsureTasks':tagged('tasks.ensure',{key:D('Id') for key in ['pageId','collectionId','titlePropertyId','statusPropertyId','duePropertyId','defaultOptionId','doneOptionId']}),
'PromoteTask':tagged('task.promote',{'recordId':D('Id'),'statusPropertyId':D('Id')}),
'SetTaskStatus':tagged('task.setStatus',{'recordId':D('Id'),'statusOptionId':D('Id')}),
'AttachFile':tagged('file.attach',{'fileId':D('Id'),'ingestHandle':D('Id'),'pageId':D('Id'),'blockId':D('Id')},['fileId','ingestHandle','pageId']),
'CreateMeeting':tagged('meeting.create',{'meetingId':D('Id'),'pageId':D('Id'),'recordingFileId':D('Id'),'startedAt':D('Timestamp')}),
'AppendTranscript':tagged('meeting.appendSegments',{'meetingId':D('Id'),'segments':arr(obj({'segmentId':D('Id'),'blockId':D('Id'),'startMs':N,'endMs':N,'text':S,'speakerLabel':S},['segmentId','blockId','startMs','endMs','text']),256)}),
'FinalizeMeeting':tagged('meeting.finalize',{'meetingId':D('Id'),'durationMs':N}),
'ProposeMemory':tagged('memory.propose',{'memoryId':D('Id'),'pageId':D('Id'),'scope':D('Scope'),'memoryKind':enum('preference','project_context','decision'),'content':S,'pageRef':D('Id'),'sources':arr(D('Location'),64),'supersedesId':D('Id')},['memoryId','pageId','scope','memoryKind','sources']),
'ApproveMemory':tagged('memory.approve',{'memoryId':D('Id')}),
'ForgetMemory':tagged('memory.forget',{'memoryId':D('Id')}),
'UpdateMemory':tagged('memory.update',{'memoryId':D('Id'),'content':{'type':'string','maxLength':65536},'sources':arr(D('Location'),64)},['memoryId']),
'SetMemoryStatus':tagged('memory.setStatus',{'memoryId':D('Id'),'status':enum('proposed','disabled','superseded')}),
'ApplyImportPlan':tagged('import.applyPlan',{'planId':D('Id'),'planHash':{'type':'string','pattern':'^[0-9a-f]{64}$'},'resolutions':arr(D('ImportResolution'))}),
'RestoreRevision':tagged('history.restore',{'pageId':D('Id'),'restoreRevision':D('Revision'),'previewId':D('Id')}),
'ApplyTemplate':tagged('template.apply',{'previewId':D('Id')}),
'ConfigureCollection':tagged('collection.configure',{'collectionId':D('Id'),'locked':B}),
'ConfigureRecord':tagged('record.configure',{'recordId':D('Id'),'parentRecordId':{'oneOf':[D('Id'),{'type':'null'}]},'dependencyRecordIds':arr(D('Id'),128)}),
'SaveAutomation':tagged('automation.save',{'pageId':D('Id'),'rule':{'type':'object'}}),
'RunAutomation':tagged('automation.run',{'pageId':D('Id'),'ruleId':D('Id'),'occurrenceKey':{'type':'string','minLength':1,'maxLength':128},'eventId':D('Id'),'at':D('Timestamp')},['pageId','ruleId','occurrenceKey','at']),
'StateAutomation':tagged('automation.state',{'pageId':D('Id'),'ruleId':D('Id'),'enabled':B,'paused':B}),
}
# Optional P2 packages share the canonical mutation envelope and guards.
defs['P2ChartConfig']=obj({'groupPropertyId':{'oneOf':[D('Id'),{'type':'null'}]},'valuePropertyId':{'oneOf':[D('Id'),{'type':'null'}]},'aggregation':enum('count','sum','average','min','max'),'visualization':enum('bar','donut')})
defs['P2FormConfig']=obj({'enabled':B,'requiredPropertyIds':arr(D('Id'),64)})
defs['P2RowButton']=obj({'id':D('Id'),'label':{'type':'string','minLength':1,'maxLength':120},'propertyId':D('Id'),'value':R('ValueInput'),'confirmationRequired':B})
defs['P2DatabaseConfig']=obj({'chart':R('P2ChartConfig'),'form':R('P2FormConfig'),'buttons':arr(R('P2RowButton'),16)})
commands.update({
 'ConfigureP2Database':tagged('database.p2Configure',{'collectionId':D('Id'),'config':R('P2DatabaseConfig')}),
 'SubmitLocalForm':tagged('database.formSubmit',{'collectionId':D('Id'),'recordId':D('Id'),'pageId':D('Id'),'title':{'type':'string','minLength':1,'maxLength':2000},'values':arr(obj({'propertyId':D('Id'),'value':R('ValueInput')}),128)}),
 'RunRowButton':tagged('database.rowButtonRun',{'collectionId':D('Id'),'recordId':D('Id'),'buttonId':D('Id'),'confirmed':B}),
 'SetPageDisplay':tagged('page.display.set',{'pageId':D('Id'),'fullWidth':B,'presentation':B}),
})
commands.update({
 'ActivatePersonalReview':tagged('review.activate',{'pageId':D('Id'),'planBlockId':D('Id'),'enabled':B}),
 'AcceptPersonalReview':tagged('review.accept',{'pageId':D('Id'),'runId':D('Id'),'blockId':D('Id'),'approved':{'const':True}}),
 'DismissPersonalReview':tagged('review.dismiss',{'pageId':D('Id'),'runId':D('Id')}),
})
commands['CreateTask']['dependentRequired']={'collectionId':['statusPropertyId'],'statusPropertyId':['collectionId']}
commands['ProposeMemory']['oneOf']=[{'required':['content'],'not':{'required':['pageRef']}},{'required':['pageRef'],'not':{'required':['content']}}]
commands['ProposeMemory']['allOf']=[{'if':{'properties':{'memoryKind':{'const':'project_context'}}},'then':{'required':['pageRef']},'else':{'required':['content']}}]
commands['UpdateMemory']['anyOf']=[{'required':['content']},{'required':['sources']}]
# Saved automations are a bounded declarative subset, never arbitrary commands.
# Content IDs may be occurrence-local placeholders; expansion is validated again.
def automation_ids(value):
    if value == D('Id'): return {'oneOf':[D('Id'),{'type':'string','pattern':r'^\$id:[A-Za-z0-9_-]{1,80}$'}]}
    if isinstance(value,dict): return {k:automation_ids(v) for k,v in value.items()}
    if isinstance(value,list): return [automation_ids(v) for v in value]
    return value
defs['AutomationTemplateAction']=tagged('template.instantiate',{'templateId':D('Id'),'templateHash':{'type':'string','pattern':'^[0-9a-f]{64}$'},'title':{'type':'string','minLength':1,'maxLength':2000},'collectionId':D('Id')},['templateId','templateHash','title'])
defs['AutomationAction']={'oneOf':[automation_ids(commands[n]) for n in ['CreatePage','UpdatePage','CreateRecord','SetValue','ClearValue','SetTaskStatus']]+[R('AutomationTemplateAction')]}
defs['AutomationTrigger']=obj({'type':enum('event','schedule'),'collectionId':D('Id'),'entityType':enum('page','block','collection','property','record','view','container','file','meeting','transcript_segment','memory'),'timezone':S,'frequency':enum('daily','weekly'),'hour':{'type':'integer','minimum':0,'maximum':23},'minute':{'type':'integer','minimum':0,'maximum':59},'weekday':{'type':'integer','minimum':0,'maximum':6},'missedPolicy':enum('skip','catch_up'),'catchUpLimit':{'type':'integer','minimum':1,'maximum':20}},['type'])
defs['AutomationCondition']=obj({'recordId':D('Id'),'propertyId':D('Id'),'op':enum('eq','ne','is_empty'),'value':json_value},['recordId','propertyId','op'])
defs['AutomationRule']=obj({'id':D('Id'),'name':{'type':'string','minLength':1,'maxLength':200},'enabled':B,'paused':B,'allowedOrigins':arr(enum('user','ai'),2),'trigger':R('AutomationTrigger'),'condition':R('AutomationCondition'),'actions':{**arr(R('AutomationAction'),32),'minItems':1}},['id','name','enabled','paused','trigger','actions'])
commands['SaveAutomation']['properties']['rule']=R('AutomationRule')
defs.update(commands)
defs['Command']={'oneOf':[R(n) for n in commands]}
header={'contractVersion':{'const':1},'requestId':D('Id'),'workspaceId':D('Id')}
defs['AutomationOptionsRequest']=obj({**header,'pageId':D('Id')})
defs['TemplatePropertyDecision']={'oneOf':[obj({'sourcePropertyId':D('Id'),'action':{'const':'omit'}}),obj({'sourcePropertyId':D('Id'),'action':{'const':'map'},'targetPropertyId':D('Id'),'value':json_value})]}
defs['TemplatePreviewRequest']=obj({**header,'templateId':D('Id'),'title':{'type':'string','minLength':1,'maxLength':2000},'collectionId':D('Id'),'propertyDecisions':arr(R('TemplatePropertyDecision'),256)},[*header,'templateId','title'])
defs['TemplatePropertyIssue']=obj({'sourceProperty':D('PropertyDefinition'),'value':json_value,'reason':S})
defs['TemplatePreviewResult']=obj({'previewId':D('Id'),'pageId':D('Id'),'recordId':D('Id'),'expiresAt':D('Timestamp'),'expectedRevisions':arr(R('ExpectedRevision')),'command':R('ApplyTemplate'),'blockCount':N,'valueCount':N,'taskCount':N,'warnings':arr(S),'propertyIssues':arr(R('TemplatePropertyIssue'),256),'targetProperties':arr(D('PropertyDefinition'),256),'canApply':B},['previewId','pageId','expiresAt','expectedRevisions','command','blockCount','valueCount','taskCount','warnings','propertyIssues','targetProperties','canApply'])
defs['TemplateCaptureRequest']=obj({**header,'pageId':D('Id'),'name':{'type':'string','minLength':1,'maxLength':200},'expectedPageRevision':D('Revision')})
defs['DatabaseP2Request']=obj({**header,'collectionId':D('Id')})
defs['DatabaseChartRequest']=obj({**header,'collectionId':D('Id')})
defs['PageDisplayGetRequest']=obj({**header,'pageId':D('Id')})
defs['GraphQueryRequest']=obj({**header,'pageId':D('Id'),'query':{'type':'string','maxLength':8192},'includeSemantic':B,'limit':{'type':'integer','minimum':1,'maximum':100}},list(header))
defs['OCRStatusRequest']=obj(header)
defs['OCRRunRequest']=obj({**header,'fileId':D('Id')})
# Human references and values become canonical commands only in the trusted planner.
plan_name={'type':'string','minLength':1,'maxLength':512}
plan_body={'type':'string','maxLength':16000}
plan_actions={
 'PlanCreatePage':tagged('create_page',{'destination':plan_name,'title':plan_name,'body':plan_body},['destination','title']),
 'PlanCreateDatabase':tagged('create_database',{'destination':plan_name,'title':plan_name,'body':plan_body,'properties':arr(obj({'name':{'type':'string','minLength':1,'maxLength':128},'type':enum('text','number','checkbox','date','url','email','phone')}),32)},['destination','title']),
 'PlanUpdatePage':tagged('update_page',{'target':plan_name,'title':plan_name,'body':{'type':'string','minLength':1,'maxLength':16000}},['target']),
 'PlanCreateTask':tagged('create_task',{'destination':plan_name,'title':plan_name,'body':plan_body,'status':plan_name,'due':{'type':'string','pattern':'^[0-9]{4}-[0-9]{2}-[0-9]{2}$'}},['destination','title']),
 'PlanCreateRecord':tagged('create_record',{'destination':plan_name,'title':plan_name,'body':plan_body},['destination','title']),
 'PlanUpdateRecord':tagged('update_record',{'target':plan_name,'property':plan_name,'value':json_value}),
 'PlanUpdateTask':tagged('update_task',{'target':plan_name,'status':plan_name,'due':{'type':'string','pattern':'^[0-9]{4}-[0-9]{2}-[0-9]{2}$'}},['target']),
}
plan_actions['PlanUpdatePage']['anyOf']=[{'required':['title']},{'required':['body']}]
plan_actions['PlanUpdateTask']['anyOf']=[{'required':['status']},{'required':['due']}]
defs.update(plan_actions)
defs['PlannedAction']={'oneOf':[R(n) for n in plan_actions]}
defs['ActionPlan']=obj({'version':{'const':1},'actions':{**arr(R('PlannedAction'),32),'minItems':1}})
defs['ReviewListRequest']=obj(header)
defs['ReviewRunRequest']=obj({**header,'planBlockId':D('Id')})
defs['BatchRequest']=obj({**header,'mutation':R('MutationMeta'),'commands':{**arr(R('Command'),256),'minItems':1},'cancellationId':D('Id')},[*header,'mutation','commands'])
defs['RevisionChange']=obj({'target':R('EntityRef'),'before':{'oneOf':[D('Revision'),{'const':'0'}]},'after':D('Revision'),'deleted':B})
defs['BatchResult']=obj({'requestId':D('Id'),'transactionId':D('Id'),'eventCursor':D('Revision'),'revisions':arr(R('RevisionChange')),'replayed':B})
defs['BatchResponse']={'oneOf':[obj({'ok':{'const':True},'result':R('BatchResult')}),obj({'ok':{'const':False},'error':R('Error')})]}
defs['CommittedChangeEvent']=obj({'contractVersion':{'const':1},'eventId':D('Id'),'workspaceId':D('Id'),'transactionId':D('Id'),'cursor':D('Revision'),'committedAt':D('Timestamp'),'context':R('ExecutionContext'),'revisions':arr(R('RevisionChange')),'mutationHistoryId':D('Id'),'idempotencyKey':S})
defs['MutationHistory']=obj({'historyId':D('Id'),'transactionId':D('Id'),'requestId':D('Id'),'context':R('ExecutionContext'),'approvalId':D('Id'),'commandHash':S,'idempotencyKey':S,'changes':arr(R('RevisionChange')),'beforeSnapshotRefs':arr(D('Id')),'afterSnapshotRefs':arr(D('Id')),'committedAt':D('Timestamp')},['historyId','transactionId','requestId','context','commandHash','idempotencyKey','changes','beforeSnapshotRefs','afterSnapshotRefs','committedAt'])
defs['AllocateIdsRequest']=obj({**header,'count':{'type':'integer','minimum':1,'maximum':1000}})
defs['AllocateIdsResult']=obj({'ids':arr(D('Id'),1000)})
defs['WorkspaceSnapshotRequest']=obj({**header,'contentMode':enum('full','shell')},[*header])
defs['ReadBlockRequest']=obj({**header,'blockId':D('Id')})
defs['ReadBlockResult']=obj({'block':D('Block')})
defs['ReadRecordRequest']=obj({**header,'recordId':D('Id')})
defs['ReadRecordResult']=obj({'record':D('Record')})
defs['DatabaseStructureRequest']=obj({**header,'collectionId':D('Id')})
defs['BookmarkPreviewRequest']=obj({**header,'pageId':D('Id'),'url':{'type':'string','minLength':1,'maxLength':8192},'consent':{'const':'fetch_once'}})
defs['BookmarkPreviewResult']=obj({'sourceUrl':{'type':'string','maxLength':8192},'title':{'type':'string','maxLength':512},'description':{'type':'string','maxLength':2048},'fetchedAt':D('Timestamp')})
defs['FormulaRegistryRequest']=obj(header)
defs['FormulaPreviewRequest']=obj({**header,'propertyId':D('Id'),'recordId':D('Id'),'expression':{'type':'string','maxLength':8192},'evaluatorVersion':{'const':2},'at':D('Timestamp'),'timezone':{'type':'string','minLength':1,'maxLength':128}})
defs['BacklinksRequest']=obj({**header,'pageId':D('Id')})
defs['BacklinksResult']=obj({'links':arr(obj({'pageId':D('Id'),'blockId':D('Id')}),10000)})
defs['JobsListRequest']=obj({**header,'limit':{'type':'integer','minimum':1,'maximum':200},'state':enum('queued','running','retry_wait','cancel_requested','cancelled','succeeded','failed','interrupted')},[*header,'limit'])
defs['ReadPageRequest']=obj({**header,'pageId':D('Id'),'atRevision':D('Revision')},[*header,'pageId'])
defs['ReadPageResult']=obj({'page':D('Page'),'blocks':arr(D('Block')),'snapshotCursor':{'oneOf':[D('Revision'),{'const':'0'}]}})
defs['HistoryListRequest']=obj({**header,'pageId':D('Id')})
defs['HistoryListResult']=obj({'revisions':arr(obj({'revision':D('Revision'),'createdAt':D('Timestamp'),'deleted':B,'title':S}),10000),'snapshotCursor':{'oneOf':[D('Revision'),{'const':'0'}]}})
defs['HistoryPropertyDecision']={'oneOf':[obj({'sourcePropertyId':D('Id'),'action':{'const':'retain'}}),obj({'sourcePropertyId':D('Id'),'action':{'const':'map'},'targetPropertyId':D('Id'),'value':json_value})]}
defs['HistoryPropertyDefinition']=obj({'id':D('Id'),'collectionId':D('Id'),'name':S,'type':S,'schemaVersion':{'type':'integer','minimum':1,'maximum':9007199254740991},'config':json_value,'revision':D('Revision')})
defs['HistoryHistoricalValueIssue']=obj({'historicalValue':D('PropertyValue'),'historicalProperty':R('HistoryPropertyDefinition'),'currentProperty':R('HistoryPropertyDefinition'),'reason':S},['historicalValue','reason'])
defs['HistoryPreviewRestoreRequest']=obj({**header,'pageId':D('Id'),'restoreRevision':D('Revision'),'propertyDecisions':arr(R('HistoryPropertyDecision'),256)},[*header,'pageId','restoreRevision'])
defs['HistoryPreviewRestoreResult']=obj({'previewId':D('Id'),'pageId':D('Id'),'restoreRevision':D('Revision'),'expiresAt':D('Timestamp'),'expectedRevisions':arr(R('ExpectedRevision')),'affectedFileIds':arr(D('Id')),'snapshot':R('ReadPageResult'),'warnings':arr(S),'incompatibleValues':arr(R('HistoryHistoricalValueIssue'),256),'targetProperties':arr(R('HistoryPropertyDefinition'),256),'canRestore':B},['previewId','pageId','restoreRevision','expiresAt','expectedRevisions','affectedFileIds','snapshot','warnings'])
defs['HistoryTrashRequest']=obj(header)
defs['HistoryTrashResult']=obj({'pages':arr(D('Page'),10000)})
defs['HistoryRecoveryPointRequest']=obj({**header,'label':{'type':'string','maxLength':256},'idempotencyKey':{'type':'string','minLength':1,'maxLength':128}})
defs['HistoryRecoveryPointResult']=obj({'recoveryPointId':D('Id'),'createdAt':D('Timestamp'),'eventCursor':{'oneOf':[D('Revision'),{'const':'0'}]}})
defs['QueryRecordsRequest']=obj({**header,'collectionId':D('Id'),'viewId':D('Id'),'cursor':S,'limit':{'type':'integer','minimum':1,'maximum':200}},[*header,'collectionId','limit'])
defs['QueryRecordsResult']=obj({'recordIds':arr(D('Id'),200),'records':arr(D('Record'),200),'nextCursor':S,'snapshotCursor':{'oneOf':[D('Revision'),{'const':'0'}]}},['recordIds','records','snapshotCursor'])
defs['SearchRequest']=obj({**header,'query':{'type':'string','maxLength':8192},'scope':D('Scope'),'limit':{'type':'integer','minimum':1,'maximum':100},'mode':enum('keyword','hybrid')})
defs['SearchHit']=obj({'location':D('Location'),'excerpt':S,'contentHash':{'type':'string','pattern':'^[0-9a-f]{64}$'},'score':{'type':'number'},'sourceState':enum('current','stale','deleted','unavailable')})
defs['SearchResult']=obj({'hits':arr(R('SearchHit'),100),'snapshotCursor':{'oneOf':[D('Revision'),{'const':'0'}]},'indexGeneration':D('Id'),'partial':B})
defs['ResolveCitationRequest']=obj({**header,'location':D('Location')})
defs['ResolveCitationResult']=obj({'location':D('Location'),'state':enum('current','stale','deleted','unavailable'),'currentRevision':D('Revision'),'excerpt':S,'historicalSnapshotRef':D('Id')},['location','state'])
defs['ReadEventsRequest']=obj({**header,'afterCursor':{'oneOf':[D('Revision'),{'const':'0'}]},'limit':{'type':'integer','minimum':1,'maximum':500}})
defs['ReadEventsResult']=obj({'events':arr(R('CommittedChangeEvent'),500),'nextCursor':{'oneOf':[D('Revision'),{'const':'0'}]},'requiresSnapshot':B})
defs['CancelRequest']=obj({**header,'cancellationId':D('Id')})
defs['CancelResult']=obj({'cancellationId':D('Id'),'state':enum('requested','cancelled','already_committed','not_found'),'transactionId':D('Id')},['cancellationId','state'])
defs['PrepareFileRequest']=obj({**header,'selectionHandle':D('Id')})
defs['PrepareFileResult']=obj({'ingestHandle':D('Id'),'contentHash':D('Hash'),'sizeBytes':{'type':'string','pattern':'^(0|[1-9][0-9]{0,18})$'},'mediaType':S,'fileId':D('Id'),'expiresAt':D('Timestamp')})
# Notion capture is an owner-only network workflow. Raw source objects stay in
# persisted capture state; the public summary exposes only bounded task metadata.
notion_source_id={'type':'string','pattern':'^[0-9a-fA-F-]{32,36}$'}
defs['NotionRoot']=obj({'kind':enum('page','database','data_source'),'id':notion_source_id})
defs['NotionCaptureStartRequest']=obj({**header,'roots':{**arr(R('NotionRoot'),20),'minItems':1},'rootIds':{**arr(notion_source_id,20),'minItems':1},'sourceNamespace':{'type':'string','minLength':1,'maxLength':400}},[*header,'sourceNamespace'])
defs['NotionCaptureStartRequest']['oneOf']=[{'required':['roots'],'not':{'required':['rootIds']}},{'required':['rootIds'],'not':{'required':['roots']}}]
defs['NotionCaptureStatusRequest']=obj({**header,'captureId':D('Id')})
defs['NotionCaptureListRequest']=obj(header)
defs['NotionCaptureStepRequest']=obj({**header,'captureId':D('Id'),'revision':{'type':'integer','minimum':1},'token':{'type':'string','minLength':8,'maxLength':4096}})
defs['NotionCaptureFinishRequest']=obj({**header,'captureId':D('Id'),'revision':{'type':'integer','minimum':1},'allowPartial':B},[*header,'captureId','revision'])
defs['NotionCaptureTask']=obj({'kind':enum('page','children','database','data_source','query','property','views','view','attachment','templates'),'id':S,'pageId':S,'parentId':S,'sourceId':S,'propertyId':S,'fileIndex':N,'template':B,'cursor':{'type':'string','maxLength':4096},'depth':N},['kind','id','pageId','parentId','depth'])
defs['NotionCaptureResult']=obj({'captureId':D('Id'),'revision':{'type':'integer','minimum':1},'state':enum('capturing','blocked','ready','finished'),'authorizedRoots':arr(S,20),'apiVersion':enum('2025-09-03','2026-03-11'),'planId':D('Id'),'pending':arr(R('NotionCaptureTask'),10000),'completed':N,'bytes':N,'error':S},['captureId','revision','state','authorizedRoots','apiVersion','planId','pending','completed','bytes'])
defs['ImportItemOutcome']=obj({'sourceLocator':S,'mappingId':D('Id'),'outcome':enum('preserved','adapted','partial','unsupported_but_retained','failed'),'reason':S,'retainedFileId':D('Id'),'location':D('Location')},['sourceLocator','outcome','reason'])
defs['ImportReport']=obj({'runId':D('Id'),'jobId':D('Id'),'state':enum('preview','running','partial','completed','failed','cancelled'),'sourceCompleteness':enum('complete','partial','unknown'),'discoveredCount':N,'accountedCount':N,'items':arr(R('ImportItemOutcome'),10000),'committedBatchIds':arr(D('Id'),10000),'unresolvedSourceIds':arr(S,10000),'sourceOmissions':arr(S,10000)})
defs['PreviewImportRequest']=obj({**header,'sourceHandle':D('Id'),'sourceNamespace':S,'channel':enum('zip','html','api'),'destinationScope':D('Scope')})
defs['PreviewImportResult']=obj({'planId':D('Id'),'planHash':D('Hash'),'report':R('ImportReport'),'reimportPlans':arr(D('ReimportPlan'),10000),'requiredRevisions':arr(R('ExpectedRevision'),10000)})
defs['GetImportReportRequest']=obj({**header,'runId':D('Id')})
defs['GetImportReportResult']=R('ImportReport')
defs['ListMemoryRequest']=obj({**header,'scope':D('Scope'),'includeInactive':B})
defs['ListMemoryResult']=obj({'entries':arr(D('Memory'),1000),'snapshotCursor':{'oneOf':[D('Revision'),{'const':'0'}]}})
for method in ['AllocateIds','PreviewImport','GetImportReport','ListMemory','ReadPage','QueryRecords','Search','ResolveCitation','ReadEvents','Cancel','PrepareFile']:
    defs[method+'Response']={'oneOf':[obj({'ok':{'const':True},'result':R(method+'Result')}),obj({'ok':{'const':False},'error':R('Error')})]}
defs.update({
 'ProviderManagedStatusRequest':obj(header),
 'ProviderDiscoverRequest':obj({**header,'provider':enum('codex','claude_code')}),
 'ProviderManagedStartRequest':obj({**header,'provider':enum('codex','claude_code'),'registrationId':{'type':'string','minLength':1,'maxLength':256},'billingMode':enum('chatgpt_plan','subscription','api'),'model':{'type':'string','minLength':1,'maxLength':128},'prompt':{'type':'string','minLength':1,'maxLength':65536},'pageIds':{**arr(D('Id'),100),'minItems':1,'uniqueItems':True},'tools':{**arr(enum('readPage','search.query','queryRecords','resolveCitation','allocateIds','proposeChanges','proposeActions'),7),'minItems':1,'uniqueItems':True},'previewHash':{'type':'string','pattern':'^[a-f0-9]{64}$'},'resumeRunId':D('Id')},[*header,'provider','registrationId','billingMode','model','prompt','pageIds','tools','previewHash']),
 'ProviderManagedCancelRequest':obj({**header,'runId':D('Id')})
})
# Native account operations expose metadata only, never OAuth URLs or tokens.
defs.update({
'ProviderAuthIdentity':obj({'registrationId':D('Id'),'subject':{'type':'string','minLength':1,'maxLength':512},'clientId':{'type':'string','minLength':1,'maxLength':256},'email':{'type':'string','maxLength':512},'name':{'type':'string','maxLength':512}}),
'ProviderAuthRegistration':obj({'id':D('Id'),'identity':R('ProviderAuthIdentity'),'state':enum('signed_in','signed_out','reauthorization_required','sign_out_incomplete'),'planAuthorized':B,'expiresAt':{'type':'string','format':'date-time'}}),
'ProviderAuthAttempt':obj({'id':D('Id'),'registrationId':{'oneOf':[D('Id'),{'const':''}]},'state':enum('waiting','exchanging','completed','failed','cancelled','retry_required'),'message':S,'expiresAt':{'type':'string','format':'date-time'}}),
'ProviderAuthListRequest':obj(header),
'ProviderAuthBeginRequest':obj({**header,'registrationId':D('Id')},list(header)),
'ProviderAuthAttemptRequest':obj({**header,'attemptId':D('Id')}),
'ProviderAuthSignOutRequest':obj({**header,'registrationId':D('Id')}),
'ProviderAuthListResult':obj({'available':B,'reason':S,'registrations':arr(R('ProviderAuthRegistration'),32)}),
'ProviderAuthSignOutResult':obj({'remoteRevoked':B,'localCleared':B}),
})
save('service',schema('service',defs,'BatchRequest'))
# Jobs and private worker framing. Domain payloads are referenced instead of re-created.
j={
'JobKind':enum('IMPORT_NOTION','EXTRACT_DOCUMENT','INDEX_CONTENT','GENERATE_EMBEDDINGS','TRANSCRIBE_AUDIO','SUMMARIZE_MEETING','CREATE_THUMBNAIL','BACKUP_WORKSPACE','MODEL_PACK_ACQUIRE','INVALIDATE_MEMORY','AUTOMATION_RUN'),
'JobState':enum('queued','running','retry_wait','cancel_requested','cancelled','succeeded','failed','interrupted'),
'Progress':obj({'sequence':N,'completed':N,'total':N,'unit':enum('items','bytes','segments','tokens','steps'),'message':{'type':'string','maxLength':2048}},['sequence','completed','unit']),
'ResourceLimits':obj({'threads':{'type':'integer','minimum':1,'maximum':8},'memoryBytes':{'type':'integer','minimum':67108864,'maximum':17179869184},'timeoutMs':{'type':'integer','minimum':100,'maximum':7200000},'maxOutputBytes':{'type':'integer','minimum':1,'maximum':67108864},'maxTokens':{'type':'integer','minimum':1,'maximum':32768}}),
'InputHandle':obj({'handleId':D('Id'),'kind':enum('text','audio','model','image','embedding_batch','result'),'sha256':{'type':'string','pattern':'^[0-9a-f]{64}$'},'sizeBytes':N}),
'JobPayload':{'oneOf':[
 tagged('import',{'stagingId':D('Id'),'sourceRevision':S}),
 tagged('file',{'fileId':D('Id'),'extractorVersion':S}),
 tagged('index',{'location':D('Location'),'generationId':D('Id')}),
 tagged('embedding',{'locations':arr(D('Location'),256),'modelPackId':D('Id'),'dimension':{'type':'integer','minimum':1,'maximum':8192}}),
 tagged('transcribe',{'meetingId':D('Id'),'fileId':D('Id'),'modelPackId':D('Id')}),
 tagged('summarize',{'meetingId':D('Id'),'sourceRevision':D('Revision'),'modelPackId':D('Id')}),
 tagged('backup',{'destinationHandle':D('Id')}),
 tagged('model',{'acquisitionId':D('Id')}),
 tagged('memory',{'memoryIds':arr(D('Id'))}),
 tagged('automation',{'ruleId':D('Id'),'ruleVersion':D('Revision'),'occurrenceKey':S})]},
'Job':obj({'jobId':D('Id'),'workspaceId':D('Id'),'kind':R('JobKind'),'payloadVersion':{'const':1},'payload':R('JobPayload'),'state':R('JobState'),'revision':D('Revision'),'idempotencyKey':S,'attempt':{'type':'integer','minimum':0,'maximum':5},'maxAttempts':{'type':'integer','minimum':1,'maximum':5},'nextAttemptAt':D('Timestamp'),'leaseToken':D('Id'),'leaseExpiresAt':D('Timestamp'),'checkpointRef':D('Id'),'progress':R('Progress'),'createdAt':D('Timestamp'),'updatedAt':D('Timestamp'),'error':{'$ref':'service.schema.json#/$defs/Error'}},['jobId','workspaceId','kind','payloadVersion','payload','state','revision','idempotencyKey','attempt','maxAttempts','createdAt','updatedAt']),
'JobProgressEvent':obj({'contractVersion':{'const':1},'jobId':D('Id'),'jobRevision':D('Revision'),'attempt':{'type':'integer','minimum':0,'maximum':5},'state':R('JobState'),'progress':R('Progress')}),
'WorkerRequest':obj({'protocolVersion':{'const':1},'requestId':D('Id'),'jobId':D('Id'),'leaseToken':D('Id'),'cancellationId':D('Id'),'operation':enum('generate','embed','transcribe','tokenize'),'modelPackId':D('Id'),'limits':R('ResourceLimits'),'inputs':arr(R('InputHandle'),32)}),
'WorkerOutput':{'oneOf':[
 tagged('hello',{'protocolVersion':{'const':1},'runtime':enum('llama.cpp','whisper.cpp'),'runtimeCommit':{'type':'string','pattern':'^[0-9a-f]{40}$'},'architecture':{'const':'arm64'},'capabilities':arr(enum('generate','embed','transcribe','tokenize'),4)}),
 tagged('progress',{'requestId':D('Id'),'sequence':N,'progress':R('Progress')}),
 tagged('token',{'requestId':D('Id'),'sequence':N,'text':{'type':'string','maxLength':16384}}),
 tagged('result',{'requestId':D('Id'),'leaseToken':D('Id'),'sequence':N,'resultHandle':R('InputHandle')}),
 tagged('error',{'requestId':D('Id'),'sequence':N,'code':enum('CANCELLED','INVALID_INPUT','MODEL_INCOMPATIBLE','RESOURCE_EXHAUSTED','RUNTIME_FAILURE'),'message':{'type':'string','maxLength':2048},'retryable':B}),
 tagged('cancelled',{'requestId':D('Id'),'sequence':N})]},
'WorkerCancel':obj({'protocolVersion':{'const':1},'requestId':D('Id'),'cancellationId':D('Id'),'kind':{'const':'cancel'}}),
'WorkerResult':{'oneOf':[
 tagged('generation',{'text':{'type':'string','maxLength':1048576},'citations':arr(D('Location'),256),'finishReason':enum('eos','length')},['text','citations']),
 tagged('embedding',{'modelPackId':D('Id'),'dimension':{'type':'integer','minimum':1,'maximum':8192},'vectors':arr(arr({'type':'number'},8192),256),'locations':arr(D('Location'),256)}),
 tagged('transcript',{'meetingId':D('Id'),'segments':arr(obj({'segmentId':D('Id'),'blockId':D('Id'),'startMs':N,'endMs':N,'text':S}),256)}),
 tagged('token_counts',{'counts':arr({'type':'integer','minimum':1,'maximum':1048576},16)})]},
'EnqueueJobRequest':obj({**header,'kind':R('JobKind'),'payload':R('JobPayload'),'idempotencyKey':S}),
'GetJobRequest':obj({**header,'jobId':D('Id')}),
'CancelJobRequest':obj({**header,'jobId':D('Id'),'expectedRevision':D('Revision')}),
'JobResponse':{'oneOf':[obj({'ok':{'const':True},'result':R('Job')}),obj({'ok':{'const':False},'error':{'$ref':'service.schema.json#/$defs/Error'}})]}
}
save('jobs',schema('jobs',j,'Job'))
