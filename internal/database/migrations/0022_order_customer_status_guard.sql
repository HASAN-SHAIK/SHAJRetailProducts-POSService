CREATE TRIGGER IF NOT EXISTS trg_sales_orders_reject_inactive_customer_insert
BEFORE INSERT ON sales_orders
FOR EACH ROW
WHEN NEW.customer_id IS NOT NULL
 AND EXISTS (
    SELECT 1
    FROM customers
    WHERE id = NEW.customer_id
      AND status = 'inactive'
 )
BEGIN
    SELECT RAISE(ABORT, 'customer_inactive');
END;

CREATE TRIGGER IF NOT EXISTS trg_sales_orders_reject_inactive_customer_update
BEFORE UPDATE OF customer_id ON sales_orders
FOR EACH ROW
WHEN NEW.customer_id IS NOT NULL
 AND EXISTS (
    SELECT 1
    FROM customers
    WHERE id = NEW.customer_id
      AND status = 'inactive'
 )
BEGIN
    SELECT RAISE(ABORT, 'customer_inactive');
END;
