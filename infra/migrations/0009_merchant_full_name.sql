-- ADR-0039：支付单据的商户全称。
-- merchant 保持为账单列表中辨认用的显示名；merchant_full_name 保存票面上更完整的
-- 商户名称，供报销与对账使用。并非所有支付截图都印出全称，因此可空。
-- 判重与报销快照约束继续以 merchant 为准，本列不参与。
ALTER TABLE payments ADD COLUMN merchant_full_name TEXT;
