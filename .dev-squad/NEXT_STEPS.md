# v0.0.27: Immediate Next Steps

## TL;DR
v0.0.27 is released and ready. The panic fix from v0.0.24 didn't work (root cause: ApplyPreset overwrites config). v0.0.27 fixes it at the spec level (proven in unit tests). **Deploy to foxhound-serp-scraper and validate under real load for 24h.**

---

## Today (Immediate Actions)

### 1. Update Dependency (5 min)
```bash
cd /path/to/foxhound-serp-scraper
go get github.com/sadewadee/foxhound@v0.0.27
go mod tidy
git add go.mod go.sum
git commit -m "upgrade: foxhound v0.0.27 (TLS renegotiation panic fix)"
```

### 2. Verify Build Passes (10 min)
```bash
go build ./...
docker build -t foxhound-serp-scraper:v0.0.27-niche .
```

### 3. Deploy to Staging (5 min)
```bash
docker compose -f docker-compose.staging.yaml up -d --scale enrich=2
```

### 4. Monitor for Panics (first 30 min)
```bash
# Terminal 1: Watch for panics
docker logs -f enrich | grep -iE "(panic|sessionController|renegotiat)"

# Terminal 2: Watch for success metrics
docker logs -f enrich | grep -E "(completed|processed|error)" | head -20
```

**Expected**: Zero panic lines should appear.

---

## This Week (Validation Phases)

### Phase 1: Staging Soak (2-4 hours today)
- ✅ Deployment: complete above
- Target: **0 panics**, normal latency, stable memory
- Go/No-go: If panic appears → STOP, revert, investigate. If 0 panics → proceed to Phase 2.

### Phase 2: Extended Canary (4-12 hours, any time this week)
```bash
# Keep staging running from Phase 1
# (no changes needed, just monitor longer)

# After 4h stable, do a spot check:
docker logs enrich --since 4h | grep -c "panic"  # Should be 0
docker logs enrich --since 4h | grep -c "error"  # Should be <2% of total jobs
```

### Phase 3: Production Gradual (Thursday or Friday)
```bash
# Start with reduced capacity
docker compose -f docker-compose.yaml up -d --scale enrich=2
# Monitor for 6 hours, then:
docker compose -f docker-compose.yaml up -d --scale enrich=4
```

**Target**: 24h panic-free run. If achieved → issue #43 is ready to close.

---

## Reference Documents

| Document | Purpose | Read if... |
|----------|---------|-----------|
| `.dev-squad/v0.0.27-RELEASE_SUMMARY.md` | What v0.0.27 contains, root cause explanation | You want to understand the fix |
| `.dev-squad/v0.0.27-production-validation.md` | Detailed validation procedures, monitoring queries, failure scenarios | You're running staging/prod validation |
| `.dev-squad/DEPLOYMENT_CHECKLIST.md` | Phase-by-phase checklist with go/no-go criteria | You're managing the deployment |
| `.dev-squad/gotchas.md` | Lessons learned from v0.0.24 no-op | You want to prevent similar issues |
| `CHANGELOG.md` | Official release notes for v0.0.27 | You need to update customer-facing docs |

---

## Success Criteria

| Phase | Duration | Panic Count | Action if ✅ | Action if ❌ |
|-------|----------|-------------|------------|------------|
| Staging Canary | 2-4h | **0** | Proceed to Phase 2 | Revert & investigate |
| Extended Canary | 4-12h | **0** | Proceed to Phase 3 | Revert & investigate |
| Prod Gradual | 24h+ | **0** | Close issue #43 | Escalate & rollback |

---

## Failure Handling

**If panic occurs at any phase:**
1. Note the time and full panic stack
2. Revert: `go get github.com/sadewadee/foxhound@v0.0.26 && go mod tidy`
3. Rebuild and redeploy
4. Comment on GitHub issue #43 with:
   - Exact foxhound version in binary (not just go.mod)
   - Full panic stack
   - Load profile at time of panic
   - Time-to-panic after deployment

---

## Stakeholders & Notifications

- **Dev team**: Run deployment phases above (today, this week)
- **Ops team**: Monitor panic counts during production rollout
- **Platform/SRE**: No urgent action, await validation completion
- **QA**: Can participate in extended canary monitoring if available

---

## Sign-Off

Once 24h production validation passes, comment on issue #43:
```
## ✅ Production Validation PASSED — v0.0.27 Ready

**Date**: [TODAY'S DATE]  
**Duration**: 24h+ concurrent enrich load  
**Panic Count**: 0  
**Deployment**: foxhound-serp-scraper:v0.0.27-niche  
**Success Rate**: [%]  

Fix confirmed stable. Issue #43 can now be closed.
```

Then close the issue.

---

## Questions?

Refer to `.dev-squad/v0.0.27-production-validation.md` (Failure Scenarios & Troubleshooting section) for detailed debugging guides.
