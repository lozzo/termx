INSERT INTO plan_catalog_versions(catalog_version, state, created_at, published_at)
VALUES (2, 'published', now(), now());

UPDATE plan_catalog_versions SET state='retired' WHERE catalog_version=1;
UPDATE plans SET state='retired', revision=revision+1 WHERE catalog_version=1 AND state='published';

INSERT INTO plans(
    plan_id, version, catalog_version, name, description, state, billing_period_days,
    managed_p2p_enabled, managed_p2p_max_concurrency, relay_enabled, relay_max_concurrency,
    relay_max_bytes_per_period, relay_max_bytes_per_lease, relay_max_rate_bytes_per_second,
    cloud_daemon_limit, allowed_regions, revision, created_at, published_at
) VALUES
    ('starter', 2, 2, 'Cloud Free', '托管 P2P 与偶发受限网络所需的应急 Relay。', 'published', 30,
     true, 0, true, 1, 209715200, 52428800, 250000, 2, ARRAY['*'], 1, now(), now()),
    ('professional', 2, 2, 'Personal Pro', '面向长期个人远程工作的完整 Cloud 连接体验。', 'published', 30,
     true, 0, true, 3, 21474836480, 1073741824, 1250000, 10, ARRAY['*'], 1, now(), now()),
    ('team', 2, 2, 'Power User', '面向多设备和高频 Relay 使用的更高容量。', 'published', 30,
     true, 0, true, 8, 107374182400, 5368709120, 3750000, 30, ARRAY['*'], 1, now(), now());

INSERT INTO plan_prices(plan_id, plan_version, billing_cycle, currency, minor_units) VALUES
    ('starter', 2, 'monthly', 'CNY', 0),
    ('starter', 2, 'yearly', 'CNY', 0),
    ('professional', 2, 'monthly', 'CNY', 6800),
    ('professional', 2, 'yearly', 'CNY', 64800),
    ('team', 2, 'monthly', 'CNY', 13800),
    ('team', 2, 'yearly', 'CNY', 128800);

UPDATE subscriptions
SET plan_version=2, revision=revision+1, updated_at=now()
WHERE (plan_id, plan_version) IN (('starter', 1), ('professional', 1), ('team', 1));
