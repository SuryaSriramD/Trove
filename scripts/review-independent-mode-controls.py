#!/usr/bin/env python3
"""Prepare locked manual criteria or validate a completed native-output assessment.

This command performs no inference, source edits, output repair or retry. A filled
receipt records an identified reviewer's judgement; it cannot prove that judgement
or substitute for independent human usability/release acceptance.
"""
import argparse
import hashlib
import importlib.util
import json
import pathlib
import sys
sys.dont_write_bytecode = True


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root',required=True)
    parser.add_argument('--output',required=True)
    group=parser.add_mutually_exclusive_group(required=True)
    group.add_argument('--prepare-preflight',action='store_true')
    group.add_argument('--manifest')
    parser.add_argument('--receipt')
    args=parser.parse_args()
    root=pathlib.Path(args.root).resolve()
    path=root/'scripts/run-independent-mode-controls.py'
    spec=importlib.util.spec_from_file_location('mode_controls',path)
    runner=importlib.util.module_from_spec(spec); spec.loader.exec_module(runner)
    validator=runner.load_repository_validator(root)
    raw=(root/runner.CORPUS).read_bytes()
    if hashlib.sha256(raw).hexdigest()!=runner.CORPUS_SHA256: parser.error('sealed corpus changed')
    cases=json.loads(raw,object_pairs_hook=validator._unique_json)['cases']
    if [c['id'] for c in cases]!=runner.IDS: parser.error('sealed case inventory changed')
    if args.prepare_preflight:
        if args.receipt: parser.error('preparation does not consume a receipt')
        output=runner.manual_template(cases)
    else:
        if not args.receipt: parser.error('native assessment requires --receipt')
        manifest_path=pathlib.Path(args.manifest).resolve()
        receipt_path=pathlib.Path(args.receipt).resolve()
        manifest=json.loads(manifest_path.read_text(),object_pairs_hook=validator._unique_json)
        if manifest.get('schemaVersion')!=1 or manifest.get('family')!='independent-mode-controls-v1' or manifest.get('state')!='finished' or manifest.get('test')!=runner.TEST or manifest.get('corpusSHA256')!=runner.CORPUS_SHA256 or manifest.get('caseIds')!=runner.IDS or manifest.get('summary',{}).get('executionComplete') is not True or manifest.get('summary',{}).get('inputsUnchanged') is not True:
            parser.error('incomplete or mismatched native run cannot receive complete manual acceptance')
        if manifest.get('stableBindings',{}).get('pilot',{}).get('sha256')!=validator.digest(path): parser.error('runner bytes differ from the native run')
        if set(manifest.get('outputs',{}))!=runner.OUTPUTS: parser.error('retained native output inventory differs')
        for name,sha in manifest['outputs'].items():
            if pathlib.Path(name).name!=name or validator.digest(manifest_path.parent/name)!=sha: parser.error('retained evidence bytes changed: '+name)
        events,event_errors=runner.read_events(manifest_path.parent/'results.jsonl')
        reports,report_errors=runner.extract_reports(events,validator)
        terminal,terminal_errors=runner.native_terminals(events)
        if event_errors or report_errors or terminal_errors: parser.error('retained raw test inventory invalid')
        template=runner.manual_template(cases,reports)
        retained_template=json.loads((manifest_path.parent/'manual-review-template.json').read_text(),object_pairs_hook=validator._unique_json)
        if retained_template!=template: parser.error('retained manual template differs from raw outputs')
        receipt=json.loads(receipt_path.read_text(),object_pairs_hook=validator._unique_json)
        errors=runner.validate_manual_review(receipt,template)
        if errors: parser.error('incomplete or mismatched manual receipt: '+str(errors))
        preflight=json.loads((manifest_path.parent/'manual-preflight-review.json').read_text(),object_pairs_hook=validator._unique_json)
        if runner.validate_manual_review(preflight,runner.manual_template(cases)): parser.error('retained preflight receipt invalid')
        automatic=[]
        for case in cases:
            report=reports[case['id']]
            try: automatic += [case['id']+': '+err for err in runner.validate_mode(report,case,validator)]
            except (ValueError,KeyError,TypeError,AttributeError) as error: automatic.append(case['id']+': invalid report '+str(error))
            error=validator.validate_canonical_inventory(report,manifest_path.parent/'canonical',runner.TEST+'/'+case['id'])
            if error: automatic.append(case['id']+': '+error)
            if terminal.get(runner.TEST+'/'+case['id'])!='pass': automatic.append(case['id']+': native test did not pass')
        parsed,errors=runner.read_parser_report(manifest_path.parent/'mermaid-results.json',validator)
        automatic+=errors
        entry_sha=manifest.get('inputs',{}).get(parsed.get('parserEntry'),{}).get('sha256')
        automatic+=runner.parser_report_errors(parsed,cases,'outputs',entry_sha,reports)
        if terminal.get(runner.TEST)!='pass' or manifest.get('exitCode')!=0 or manifest.get('timedOut') is not False: automatic.append('native parent/process did not pass')
        accepted=[item['fixture'] for item in receipt['items'] if item['review']['accept']]
        rejected=[item['fixture'] for item in receipt['items'] if not item['review']['accept']]
        output={'schemaVersion':1,'family':'independent-mode-controls-manual-assessment-v1','sourceCommit':manifest['sourceCommit'],'corpusSHA256':runner.CORPUS_SHA256,'manifest':str(manifest_path),'manifestSHA256':validator.digest(manifest_path),'receipt':str(receipt_path),'receiptSHA256':validator.digest(receipt_path),'reviewedCases':16,'manualAccepted':accepted,'manualRejected':rejected,'automaticErrors':automatic,'allReviewedCriteriaAccepted':not rejected and not automatic,'automaticSemanticAcceptance':False,'humanUsabilityQualified':False,'performanceQualified':False,'releaseQualified':False,'limitations':'Receipt consistency and declared all-clause coverage are checked; the software does not establish correctness or independence of reviewer judgement.'}
    out=pathlib.Path(args.output).resolve()
    if out.is_relative_to(root): parser.error('write review artifacts outside the source checkout')
    with out.open('x') as stream: json.dump(output,stream,indent=2); stream.write('\n')
    print(str(out))
    return 0


if __name__=='__main__':
    try: raise SystemExit(main())
    except (OSError,ValueError,KeyError,TypeError) as error:
        print('Review stopped without acceptance: '+str(error),file=sys.stderr)
        raise SystemExit(1)
