# Launch checklist — not ready

## Product work complete locally

- [x] PostgreSQL-backed identity, profile, schedule, quote/hold, offer and booking flows.
- [x] Local-only, clearly synthetic payment simulator; live collection remains off.
- [x] MFA-gated operations views, audited limited actions, overdue meeting-link queue and participant issue/completion flows.
- [x] Authenticated provider checkout/return, server verification, signed durable webhook inbox, retrying event worker, atomic verified payment allocation, immutable ledger, exception capture, receipt and provider health views are implemented; provider sandbox remains unverified.
- [x] Core cancellation request, rescheduling, confirmation/reminder outbox and verified paid receipt flows are implemented.
- [x] Seven-day real event aggregates and protected operations gate status are shown without implying unverified conversion.
- [x] Emergency new-checkout pause is available through protected deployment configuration; existing provider event processing remains enabled.

## Still required before a private beta

- [ ] Founder approves final name/domain and legal entity.
- [ ] Founder approves fee interpretation, transaction limits, subsidy policy, seller obligations/remedies, the escrow-and-payout route, the 2-hour problem window and the 3-hour payout time.
- [ ] Kora merchant account approved for collecting and paying out (marketplace/escrow use confirmed in writing); sandbox keys supplied; transfer checkout, card checkout, signed webhook, short/late/double payments, refunds and payouts demonstrated in the Kora sandbox.
- [ ] Approved terms, privacy, acceptable use, support details and retention periods supplied.
- [x] Pin SvelteKit Node adapter and implement web/API containers plus same-origin Nginx gateway in Compose. Production hosting, TLS and operator-managed deployment remain open.
- [ ] Provision production PostgreSQL and secrets; verify migrations, backups and restore.
- [ ] Configure verified email sender and run delivery/session staging checks.
- [ ] Apply migration 011 and review any earlier CSV-only records downgraded from settled to pending.
- [ ] Apply migration 012 (TOTP replay guard, `offer_conflict` exception kind, non-charge provider events marked ignored) and grant `ops:seller:approve` to the operators who approve seller payouts.
- [ ] Confirm the normalized settlement CSV schema matches an approved provider account export, reconcile sandbox statements, and add independently authenticated provider settlement confirmation before labeling a payout settled. An operator-uploaded CSV is provisional evidence only.
- [ ] Complete approved refund execution and dispute response/evidence submission only after provider rules and permissions are confirmed.
- [ ] Exercise meeting notification, rescheduling/cancellation, receipt delivery and operations queues against staging data.
- [ ] Exercise owner MFA bootstrap and all privileged paths in staging.
- [ ] Review public, booking, app and ops journeys visually at required widths and with keyboard/screen reader.
- [ ] Approve deployment, rollback, monitoring, support, incident response and domain setup.
- [ ] Owner explicitly authorizes private beta deployment and any live collection.

Until applicable gates pass, keep live payments off. Core payment code exists, but without sandbox evidence and owner/provider approval this system is not a launch candidate.
