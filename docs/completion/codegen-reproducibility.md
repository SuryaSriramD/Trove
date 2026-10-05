# Reproducible contract and requirements generation

The combined tooling suite reproduced three fresh-checkout defects: worker schema authoring reverted Windows architecture support, type generation did not apply the formatter used by committed Go consumers, and ignored PRD/TRD extracts were absent or reconstructed with different blank lines/trailing-newline bytes.

The authoring script now retains arm64/amd64. Type generation resolves gofmt from the pinned Go installation before publishing consumers. Both generators support --check: they compute expected bytes and report drift without rewriting artifacts. Ordinary generation prepares all output first and publishes changed files atomically. Drift tests now use the read-only check path.

Ledger setup validates the original document digests and deterministically reconstructs all paragraphs, including empty paragraphs, without adding a final newline. This reproduces the pre-existing text hashes and line anchors exactly. A different existing extract is refused for inspection. Original Word documents and accepted ledger expectations are unchanged.

All 93 Python tooling/contract/ledger tests pass. Initial drift/missing-extract failures and the intermediate extraction failure are retained beside the [source manifest](../../evidence/completion/codegen-reproducibility/manifest.json). These are development reproducibility checks, not platform release qualification.
