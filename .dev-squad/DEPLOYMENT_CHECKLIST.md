# v0.0.27 Deployment Checklist

## Pre-Deployment (foxhound repo)
- ✅ v0.0.27 committed and tagged on main
- ✅ Issue #43 reopened with root-cause analysis (v0.0.24 was a no-op)
- ✅ CHANGELOG, README, CLAUDE.md updated
- ✅ Regression test added: `TestNewStealth_GetClientHelloSpec_ForcesRenegotiateNever`
- ✅ All tests pass locally: `go test -race ./...`
- ✅ Docker image ready: `go build -tags tls ./cmd/foxhound/`

## Consumer Deployment (foxhound-serp-scraper)

### Phase 1: Dependency Update (30 min)
```bash
cd foxhound-serp-scraper
go get github.com/sadewadee/foxhound@v0.0.27
go mod tidy
```
- [ ] CI/CD pipeline passes with new dependency
- [ ] Build artifacts updated

### Phase 2: Staging Canary (2-4 hours)
```bash
# Deploy to staging with 2 enrich workers
docker compose -f docker-compose.staging.yaml up -d --scale enrich=2
# Monitor for 30+ minutes
docker compose logs -f enrich | grep -iE "(panic|renegotiat|error|tls)"
```
- [ ] Zero panics in first 30 minutes
- [ ] Job processing latency normal (±5%)
- [ ] Memory consumption stable

### Phase 3: Extended Canary (4-12 hours)
Continue staging soak test with moderate concurrent load:
- [ ] 0 panics after 4 hours
- [ ] Error rate <2%
- [ ] CPU/Memory/Network stable
- [ ] Proxy health indicators green

### Phase 4: Production Gradual Rollout (24+ hours)
```bash
# Start with 2 workers, monitor for 6h
docker compose -f docker-compose.yaml up -d --scale enrich=2

# After 6h stable, scale to 4 workers (or normal capacity)
docker compose -f docker-compose.yaml up -d --scale enrich=4
```

**Validation targets**: See `.dev-squad/v0.0.27-production-validation.md`

| Phase | Duration | Workers | Exit Criteria | Escalation |
|-------|----------|---------|---|---|
| Staging Canary | 2-4h | 2 | 0 panics | Revert to v0.0.26 if panic occurs |
| Extended Canary | 4-12h | 2 | 0 panics, <2% errors | Investigate error spike |
| Prod Gradual | 6h | 2 | 0 panics, <1.5% errors | Scale to 4 workers |
| Prod Full Load | 24h+ | 4+ | 0 panics, <1% errors, success rate ≥98% | Close issue #43 |

## Monitoring Dashboard

### Key Metrics to Watch
1. **Panic count**: Should be **0** (was 3-5/hour on v0.0.26 production)
2. **TLS error rate**: <1%
3. **Enrich latency p99**: <4000ms (baseline: ~3500ms)
4. **Job success rate**: ≥98%
5. **Memory RSS per worker**: <200MB (stable, no growth)

### Log Search Queries
```bash
# Panic detection (should return 0 results)
docker logs <container> | grep -E "panic|sessionController|renegotiat"

# TLS error tracking
docker logs <container> | grep -iE "tls.*error|certificate|handshake"

# Success metrics
docker logs <container> | grep "enrich completed" | wc -l
docker logs <container> | grep -E "(403|429|503)" | wc -l
```

### Alert Thresholds
- Panic detected in logs → **CRITICAL** (immediate escalation)
- TLS error rate >5% → **WARNING** (may indicate unrelated issue)
- Job success rate <95% → **WARNING** (investigate error type)

## Sign-Off: Validation Complete

Once 24h production run passes **all criteria**, post on issue #43:
```markdown
## ✅ Production Validation PASSED

**Validation Date**: [DATE]  
**Duration**: 24+ hours under concurrent enrich load  
**Panic Count**: 0  
**Deployment**: foxhound-serp-scraper:v0.0.27  
**Success Rate**: [%]  
**Notes**: v0.0.27 confirmed panic-free. Ready for stable release.
```

Then **close issue #43** with this comment.

---

## Rollback Plan (If Needed)

If panic still occurs on v0.0.27:
1. Collect full panic stack + logs
2. Revert to v0.0.26: `go get github.com/sadewadee/foxhound@v0.0.26`
3. Rebuild and redeploy
4. File detailed issue on foxhound #43 with:
   - Exact version in binary (verify not cached)
   - Full panic stack
   - Load profile
   - Time-to-panic

---

## References
- **Foxhound v0.0.27**: https://github.com/sadewadee/foxhound/releases/tag/v0.0.27
- **Issue #43**: https://github.com/sadewadee/foxhound/issues/43
- **Production Validation Guide**: `.dev-squad/v0.0.27-production-validation.md`
- **Root Cause**: `.dev-squad/gotchas.md` (v0.0.24 entry)
