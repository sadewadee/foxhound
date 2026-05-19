# CloakBrowser Comparison Audit — ARCHIVED

**Status: DIAGNOSIS LOCKED — DO NOT RE-RUN**

This directory contains a one-time diagnostic audit conducted in May 2026 to identify
Camoufox-side bugs by comparing Camoufox against another browser on the same targets.

## What This Was For

The audit identified that Camoufox's `geoip=True` produced malformed BCP-47 Accept-Language
strings (frankenlocales like `en-DE`, `fr-DE`) that were flagging Camoufox as a bot on Google
email dorks and other targets. This diagnosis led to the v0.0.26 fix in `identity/profile.go`.

## Why This Is Archived

**This harness violates foxhound's Camoufox-only stance.** The comparative phases
(3, 4, 5, 6, 6b) launch a second browser alongside Camoufox. That second browser is
not part of foxhound and was used solely as a diagnostic reference to identify which
headers Camoufox was producing incorrectly.

The diagnosis is now locked (v0.0.26 ships the fix). There is no further need to run
the comparison phases.

## What To Use Instead

For all future Camoufox verification, use the Camoufox-only script:

```bash
python3 .dev-squad/cloakbrowser-audit/harness/verify_v26.py
```

This script:
- Launches Camoufox only (no second browser)
- Applies the v0.0.26 canonical Accept-Language config
- Visits Google gmail dork and Walmart search
- Confirms `accept-language` header is clean BCP-47 (no frankenlocale)
- Outputs results to `verify_v26_result.json`

## DO NOT RUN

- `harness/phase6_har.py` — runs both Camoufox and a second browser
- `harness/audit.py` — runs comparison phases against multiple targets
- Any phase log script that mentions "parity run" or comparison

These scripts exist for historical reference only.
