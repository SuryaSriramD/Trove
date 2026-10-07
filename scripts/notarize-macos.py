#!/usr/bin/env python3
"""Explicitly notarize or resume a previously Developer-ID-signed Trove candidate."""
import argparse
from pathlib import Path
from macos_signing import SigningBlocked, notarize

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--app', type=Path, required=True)
parser.add_argument('--signing-evidence', type=Path, required=True)
parser.add_argument('--notary-profile', required=True, help='Configured notarytool Keychain profile name; no secret arguments')
args = parser.parse_args()
try:
    notarize(args.app, args.signing_evidence, args.notary_profile)
except SigningBlocked as error:
    parser.exit(1, 'BLOCKED: ' + str(error) + '\n')
print('Notarized artifact frozen. Product and independent-install release gates remain separate.')
