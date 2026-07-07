-- +goose Up
-- billing-fee-1: settlements now record the hosting FEE (托管费 + 渠道托管费), not raw
-- consumption. Convert existing consumption-based settled/gross amounts and their
-- settlement ledger entries to fee by multiplying by each tenant's current hosting
-- rate. This preserves the unsettled balance: it was (consumption - settledConsumption)
-- × rate; afterward it is totalFee − settledFee with settledFee = settledConsumption ×
-- rate. Channel fee was 0 historically, so no channel adjustment is needed.
UPDATE settlements s
SET settled_usd = s.settled_usd * hr.rate,
    gross_usd   = s.gross_usd   * hr.rate
FROM (SELECT DISTINCT ON (tenant_id) tenant_id, rate FROM hosting_rates ORDER BY tenant_id, effective_from DESC) hr
WHERE hr.tenant_id = s.tenant_id;

UPDATE billing_ledger bl
SET amount_usd = bl.amount_usd * hr.rate
FROM (SELECT DISTINCT ON (tenant_id) tenant_id, rate FROM hosting_rates ORDER BY tenant_id, effective_from DESC) hr
WHERE hr.tenant_id = bl.tenant_id AND bl.type = 'settlement';

-- +goose Down
UPDATE settlements s
SET settled_usd = s.settled_usd / NULLIF(hr.rate, 0),
    gross_usd   = s.gross_usd   / NULLIF(hr.rate, 0)
FROM (SELECT DISTINCT ON (tenant_id) tenant_id, rate FROM hosting_rates ORDER BY tenant_id, effective_from DESC) hr
WHERE hr.tenant_id = s.tenant_id;

UPDATE billing_ledger bl
SET amount_usd = bl.amount_usd / NULLIF(hr.rate, 0)
FROM (SELECT DISTINCT ON (tenant_id) tenant_id, rate FROM hosting_rates ORDER BY tenant_id, effective_from DESC) hr
WHERE hr.tenant_id = bl.tenant_id AND bl.type = 'settlement';
