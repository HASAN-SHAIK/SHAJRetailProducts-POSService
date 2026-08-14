package inbox

import (
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "fmt"
    "time"

    "github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

type Service struct{ db *database.DB }
func New(db *database.DB) *Service { return &Service{db: db} }

type Message struct {
    ID            string          `json:"id"`
    Type          string          `json:"type"`
    SchemaVersion int             `json:"schema_version"`
    Source        string          `json:"source"`
    Payload       json.RawMessage `json:"payload"`
}

func (s *Service) Apply(ctx context.Context, message Message) error {
    if message.ID == "" || message.Type == "" || !json.Valid(message.Payload) { return errors.New("invalid_change_message") }
    if message.SchemaVersion == 0 { message.SchemaVersion = 1 }
    if message.Source == "" { message.Source = "central" }
    now := time.Now().UTC().Format(time.RFC3339Nano)
    return s.db.WithTx(ctx, func(tx *sql.Tx) error {
        var status string
        err := tx.QueryRowContext(ctx, `SELECT status FROM inbox_messages WHERE message_id=?`, message.ID).Scan(&status)
        if err == nil && status == "applied" { return nil }
        if err != nil && !errors.Is(err, sql.ErrNoRows) { return err }
        if _, err := tx.ExecContext(ctx, `INSERT INTO inbox_messages(message_id,message_type,schema_version,source,payload_json,status,attempt_count,received_at)
            VALUES(?,?,?,?,?,'processing',1,?) ON CONFLICT(message_id) DO UPDATE SET status='processing',attempt_count=inbox_messages.attempt_count+1,last_error=NULL`,
            message.ID,message.Type,message.SchemaVersion,message.Source,string(message.Payload),now); err != nil { return err }
        if err := applyTx(ctx, tx, message); err != nil {
            _, _ = tx.ExecContext(ctx, `UPDATE inbox_messages SET status='failed',last_error=? WHERE message_id=?`, truncate(err.Error(),1000),message.ID)
            return err
        }
        _, err = tx.ExecContext(ctx, `UPDATE inbox_messages SET status='applied',applied_at=?,last_error=NULL WHERE message_id=?`,now,message.ID)
        return err
    })
}

func applyTx(ctx context.Context, tx *sql.Tx, m Message) error {
    switch m.Type {
    case "catalog.category.upsert": return upsertCategory(ctx,tx,m.Payload)
    case "catalog.categories.snapshot": return applyCategorySnapshot(ctx,tx,m.Payload)
    case "catalog.product.upsert": return upsertProduct(ctx,tx,m.Payload)
    case "catalog.product.remove": return removeProduct(ctx,tx,m.Payload)
    case "catalog.price.upsert": return upsertPrice(ctx,tx,m.Payload)
    case "catalog.barcode.upsert": return upsertBarcode(ctx,tx,m.Payload)
    case "customer.upsert": return upsertCustomer(ctx,tx,m.Payload)
    default: return fmt.Errorf("unsupported_change_type:%s",m.Type)
    }
}

type categoryPayload struct { ID string `json:"id"`; ParentID *string `json:"parent_id"`; Name string `json:"name"`; Code *string `json:"code"`; SortOrder int `json:"sort_order"`; IsActive bool `json:"is_active"`; Version int `json:"version"`; SourceUpdatedAt *string `json:"source_updated_at"` }
func upsertCategory(ctx context.Context, tx *sql.Tx, raw []byte) error { var p categoryPayload; if json.Unmarshal(raw,&p)!=nil || p.ID=="" || p.Name=="" { return errors.New("invalid_category_payload") }; now:=time.Now().UTC().Format(time.RFC3339Nano); _,err:=tx.ExecContext(ctx,`INSERT INTO catalog_categories(id,parent_id,name,code,sort_order,is_active,version,source_updated_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET parent_id=excluded.parent_id,name=excluded.name,code=excluded.code,sort_order=excluded.sort_order,is_active=excluded.is_active,version=excluded.version,source_updated_at=excluded.source_updated_at,updated_at=excluded.updated_at WHERE excluded.version>=catalog_categories.version`,p.ID,p.ParentID,p.Name,p.Code,p.SortOrder,boolInt(p.IsActive),p.Version,p.SourceUpdatedAt,now); return err }

type categorySnapshotEntry struct { ID string `json:"id"`; Name string `json:"name"` }
type categorySnapshotPayload struct { Categories []categorySnapshotEntry `json:"categories"`; Version int `json:"version"`; SourceUpdatedAt *string `json:"source_updated_at"` }
func applyCategorySnapshot(ctx context.Context, tx *sql.Tx, raw []byte) error {
    var p categorySnapshotPayload
    if json.Unmarshal(raw,&p)!=nil || p.Version<=0 { return errors.New("invalid_category_snapshot_payload") }
    for _, category := range p.Categories { if category.ID=="" || category.Name=="" { return errors.New("invalid_category_snapshot_payload") } }
    now:=time.Now().UTC().Format(time.RFC3339Nano)
    if _,err:=tx.ExecContext(ctx,`UPDATE catalog_categories SET is_active=0,version=?,source_updated_at=?,updated_at=? WHERE version<=?`,p.Version,p.SourceUpdatedAt,now,p.Version); err!=nil { return err }
    for _,category:=range p.Categories {
        if _,err:=tx.ExecContext(ctx,`INSERT INTO catalog_categories(id,parent_id,name,code,sort_order,is_active,version,source_updated_at,updated_at) VALUES(?,NULL,?,NULL,0,1,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,is_active=1,version=excluded.version,source_updated_at=excluded.source_updated_at,updated_at=excluded.updated_at WHERE excluded.version>=catalog_categories.version`,category.ID,category.Name,p.Version,p.SourceUpdatedAt,now); err!=nil { return err }
    }
    return nil
}

type productPayload struct { ID string `json:"id"`; CategoryID *string `json:"category_id"`; SKU *string `json:"sku"`; Name string `json:"name"`; Description *string `json:"description"`; UnitOfMeasure *string `json:"unit_of_measure"`; TaxCode *string `json:"tax_code"`; IsActive bool `json:"is_active"`; AllowManualPrice bool `json:"allow_manual_price"`; TrackInventory bool `json:"track_inventory"`; Version int `json:"version"`; SourceUpdatedAt *string `json:"source_updated_at"` }
func upsertProduct(ctx context.Context,tx *sql.Tx,raw []byte) error { var p productPayload; if json.Unmarshal(raw,&p)!=nil || p.ID=="" || p.Name=="" { return errors.New("invalid_product_payload") }; now:=time.Now().UTC().Format(time.RFC3339Nano); _,err:=tx.ExecContext(ctx,`INSERT INTO catalog_products(id,category_id,sku,name,description,unit_of_measure,tax_code,is_active,allow_manual_price,track_inventory,version,source_updated_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET category_id=excluded.category_id,sku=excluded.sku,name=excluded.name,description=excluded.description,unit_of_measure=excluded.unit_of_measure,tax_code=excluded.tax_code,is_active=excluded.is_active,allow_manual_price=excluded.allow_manual_price,track_inventory=excluded.track_inventory,version=excluded.version,source_updated_at=excluded.source_updated_at,updated_at=excluded.updated_at WHERE excluded.version>=catalog_products.version`,p.ID,p.CategoryID,p.SKU,p.Name,p.Description,p.UnitOfMeasure,p.TaxCode,boolInt(p.IsActive),boolInt(p.AllowManualPrice),boolInt(p.TrackInventory),p.Version,p.SourceUpdatedAt,now); return err }

type productRemovePayload struct { ID string `json:"id"`; Version int `json:"version"`; SourceUpdatedAt *string `json:"source_updated_at"` }
func removeProduct(ctx context.Context, tx *sql.Tx, raw []byte) error {
    var p productRemovePayload
    if json.Unmarshal(raw,&p)!=nil || p.ID=="" || p.Version<=0 { return errors.New("invalid_product_remove_payload") }
    now:=time.Now().UTC().Format(time.RFC3339Nano)
    result,err:=tx.ExecContext(ctx,`UPDATE catalog_products SET is_active=0,version=?,source_updated_at=?,updated_at=? WHERE id=? AND ? >= version`,p.Version,p.SourceUpdatedAt,now,p.ID,p.Version)
    if err!=nil { return err }
    affected,err:=result.RowsAffected(); if err!=nil { return err }
    if affected==0 { return nil }
    if _,err=tx.ExecContext(ctx,`DELETE FROM catalog_barcodes WHERE product_id=?`,p.ID); err!=nil { return err }
    if _,err=tx.ExecContext(ctx,`DELETE FROM catalog_prices WHERE product_id=?`,p.ID); err!=nil { return err }
    return nil
}

type pricePayload struct { ID string `json:"id"`; ProductID string `json:"product_id"`; StoreID *string `json:"store_id"`; PriceListID *string `json:"price_list_id"`; Currency string `json:"currency"`; AmountMinor int64 `json:"amount_minor"`; TaxInclusive bool `json:"tax_inclusive"`; ValidFrom *string `json:"valid_from"`; ValidTo *string `json:"valid_to"`; Priority int `json:"priority"`; Version int `json:"version"`; SourceUpdatedAt *string `json:"source_updated_at"` }
func upsertPrice(ctx context.Context,tx *sql.Tx,raw []byte) error { var p pricePayload; if json.Unmarshal(raw,&p)!=nil || p.ID=="" || p.ProductID=="" || p.Currency=="" || p.AmountMinor<0 { return errors.New("invalid_price_payload") }; now:=time.Now().UTC().Format(time.RFC3339Nano); _,err:=tx.ExecContext(ctx,`INSERT INTO catalog_prices(id,product_id,store_id,price_list_id,currency,amount_minor,tax_inclusive,valid_from,valid_to,priority,version,source_updated_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET product_id=excluded.product_id,store_id=excluded.store_id,price_list_id=excluded.price_list_id,currency=excluded.currency,amount_minor=excluded.amount_minor,tax_inclusive=excluded.tax_inclusive,valid_from=excluded.valid_from,valid_to=excluded.valid_to,priority=excluded.priority,version=excluded.version,source_updated_at=excluded.source_updated_at,updated_at=excluded.updated_at WHERE excluded.version>=catalog_prices.version`,p.ID,p.ProductID,p.StoreID,p.PriceListID,p.Currency,p.AmountMinor,boolInt(p.TaxInclusive),p.ValidFrom,p.ValidTo,p.Priority,p.Version,p.SourceUpdatedAt,now); return err }

type barcodePayload struct { Barcode string `json:"barcode"`; ProductID string `json:"product_id"`; BarcodeType *string `json:"barcode_type"`; IsPrimary bool `json:"is_primary"` }
func upsertBarcode(ctx context.Context,tx *sql.Tx,raw []byte) error {
    var p barcodePayload
    if json.Unmarshal(raw,&p)!=nil || p.Barcode=="" || p.ProductID=="" { return errors.New("invalid_barcode_payload") }
    now:=time.Now().UTC().Format(time.RFC3339Nano)
    if p.IsPrimary {
        if _,err:=tx.ExecContext(ctx,`DELETE FROM catalog_barcodes WHERE product_id=? AND is_primary=1 AND barcode<>?`,p.ProductID,p.Barcode); err!=nil { return err }
    }
    _,err:=tx.ExecContext(ctx,`INSERT INTO catalog_barcodes(barcode,product_id,barcode_type,is_primary,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(barcode) DO UPDATE SET product_id=excluded.product_id,barcode_type=excluded.barcode_type,is_primary=excluded.is_primary,updated_at=excluded.updated_at`,p.Barcode,p.ProductID,p.BarcodeType,boolInt(p.IsPrimary),now)
    return err
}

type customerPayload struct { ID string `json:"id"`; CustomerCode *string `json:"customer_code"`; Name string `json:"name"`; Phone *string `json:"phone"`; Email *string `json:"email"`; TaxID *string `json:"tax_id"`; CreditLimitMinor int64 `json:"credit_limit_minor"`; OutstandingMinor int64 `json:"outstanding_minor"`; Currency string `json:"currency"`; Status string `json:"status"`; SourceUpdatedAt *string `json:"source_updated_at"`; CreatedAt *string `json:"created_at"` }
func upsertCustomer(ctx context.Context,tx *sql.Tx,raw []byte) error { var p customerPayload; if json.Unmarshal(raw,&p)!=nil || p.ID=="" || p.Name=="" { return errors.New("invalid_customer_payload") }; var state string; err:=tx.QueryRowContext(ctx,`SELECT sync_state FROM customers WHERE id=?`,p.ID).Scan(&state); if err==nil && state=="pending" { _,err=tx.ExecContext(ctx,`UPDATE customers SET sync_state='conflict' WHERE id=?`,p.ID); return err }; if err!=nil && !errors.Is(err,sql.ErrNoRows){return err}; if p.Currency==""{p.Currency="INR"}; if p.Status==""{p.Status="active"}; now:=time.Now().UTC().Format(time.RFC3339Nano); created:=now; if p.CreatedAt!=nil{created=*p.CreatedAt}; _,err=tx.ExecContext(ctx,`INSERT INTO customers(id,customer_code,name,phone,email,tax_id,credit_limit_minor,outstanding_minor,currency,status,source_updated_at,created_at,updated_at,local_version,sync_state) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,0,'synced') ON CONFLICT(id) DO UPDATE SET customer_code=excluded.customer_code,name=excluded.name,phone=excluded.phone,email=excluded.email,tax_id=excluded.tax_id,credit_limit_minor=excluded.credit_limit_minor,outstanding_minor=excluded.outstanding_minor,currency=excluded.currency,status=excluded.status,source_updated_at=excluded.source_updated_at,updated_at=excluded.updated_at,sync_state='synced'`,p.ID,p.CustomerCode,p.Name,p.Phone,p.Email,p.TaxID,p.CreditLimitMinor,p.OutstandingMinor,p.Currency,p.Status,p.SourceUpdatedAt,created,now); return err }

func boolInt(v bool) int { if v { return 1 }; return 0 }
func truncate(v string,n int) string { if len(v)<=n{return v}; return v[:n] }
