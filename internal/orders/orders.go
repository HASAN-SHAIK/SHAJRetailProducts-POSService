package orders

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/catalog"
	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

var (
	ErrNotFound                 = errors.New("order not found")
	ErrInvalidOrder             = errors.New("invalid order")
	ErrAlreadyComplete          = errors.New("order already completed")
	ErrPriceOverrideNotAllowed  = errors.New("price override not allowed")
	ErrDiscountNotAllowed       = errors.New("discount not allowed")
	ErrDiscountLimitExceeded    = errors.New("discount limit exceeded")
	ErrPricingPolicyUnavailable = errors.New("pricing policy unavailable")
	ErrTaxPolicyUnavailable     = errors.New("tax policy unavailable")
	ErrCustomerNotFound         = errors.New("customer not found")
)

type DiscountPolicy struct {
	Allowed    bool
	MaxPercent float64
}

type Service struct {
	db                  *database.DB
	catalog             *catalog.Repository
	priceOverridePolicy func(context.Context) (bool, error)
	discountPolicy      func(context.Context) (DiscountPolicy, error)
	taxPolicy           func(context.Context) (TaxPolicy, error)
}

func New(db *database.DB, catalogRepository *catalog.Repository) *Service {
	return &Service{db: db, catalog: catalogRepository}
}

func (s *Service) SetPriceOverridePolicy(policy func(context.Context) (bool, error)) {
	s.priceOverridePolicy = policy
}

func (s *Service) SetDiscountPolicy(policy func(context.Context) (DiscountPolicy, error)) {
	s.discountPolicy = policy
}

func (s *Service) SetTaxPolicy(policy func(context.Context) (TaxPolicy, error)) {
	s.taxPolicy = policy
}

type creatorContextKey struct{}

// WithCreatorUserID attaches the authenticated Central user identity to the
// service context. HTTP callers should derive this from the verified local POS
// session; payload-supplied staff IDs must never be trusted as authority.
func WithCreatorUserID(ctx context.Context, userID string) context.Context {
	userID = strings.TrimSpace(userID)
	if userID == "" || userID == "internal-test" {
		return ctx
	}
	return context.WithValue(ctx, creatorContextKey{}, userID)
}

func creatorUserIDFromContext(ctx context.Context) *string {
	userID, _ := ctx.Value(creatorContextKey{}).(string)
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	return &userID
}

type CreateInput struct {
	ClientOrderID string      `json:"client_order_id"`
	StoreID       string      `json:"store_id"`
	TerminalID    *string     `json:"terminal_id,omitempty"`
	CustomerID    *ExternalID `json:"customer_id,omitempty"`
	Currency      string      `json:"currency"`
	Notes         *string     `json:"notes,omitempty"`
	Items         []ItemInput `json:"items"`
}

// ExternalID accepts the central PostgreSQL API convention (numeric IDs) and
// the POS projection convention (string IDs). SQLite stores local references as
// text so outbox payloads can round-trip both central and generated IDs.
type ExternalID string

func (id *ExternalID) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		return nil
	}
	var text string
	if strings.HasPrefix(raw, `"`) {
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
	} else {
		var number json.Number
		if err := json.Unmarshal(data, &number); err != nil {
			return err
		}
		text = number.String()
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	*id = ExternalID(text)
	return nil
}

func (id *ExternalID) StringPtr() *string {
	if id == nil {
		return nil
	}
	value := strings.TrimSpace(string(*id))
	if value == "" {
		return nil
	}
	return &value
}

func (id ExternalID) String() string {
	return strings.TrimSpace(string(id))
}

type ItemInput struct {
	ProductID      ExternalID `json:"product_id"`
	Barcode        *string    `json:"barcode,omitempty"`
	QuantityMilli  int64      `json:"quantity_milli"`
	UnitPriceMinor *int64     `json:"unit_price_minor,omitempty"`
	DiscountMinor  int64      `json:"discount_minor"`
	TaxMinor       int64      `json:"tax_minor"`
}

type Order struct {
	ID                string  `json:"id"`
	ClientOrderID     string  `json:"client_order_id"`
	StoreID           string  `json:"store_id"`
	TerminalID        *string `json:"terminal_id,omitempty"`
	CustomerID        *string `json:"customer_id,omitempty"`
	CreatedByUserID   *string `json:"created_by_user_id,omitempty"`
	CompletedByUserID *string `json:"completed_by_user_id,omitempty"`
	Status            string  `json:"status"`
	Currency          string  `json:"currency"`
	SubtotalMinor     int64   `json:"subtotal_minor"`
	DiscountMinor     int64   `json:"discount_minor"`
	TaxMinor          int64   `json:"tax_minor"`
	TotalMinor        int64   `json:"total_minor"`
	Notes             *string `json:"notes,omitempty"`
	Version           int     `json:"version"`
	CompletedAt       *string `json:"completed_at,omitempty"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
	Items             []Item  `json:"items"`
}

type Item struct {
	ID                   string  `json:"id"`
	LineNo               int     `json:"line_no"`
	ProductID            string  `json:"product_id"`
	SKU                  *string `json:"sku,omitempty"`
	ProductName          string  `json:"product_name"`
	Barcode              *string `json:"barcode,omitempty"`
	CategoryIDSnapshot   *string `json:"category_id,omitempty"`
	CategoryNameSnapshot *string `json:"category_name,omitempty"`
	QuantityMilli        int64   `json:"quantity_milli"`
	UnitPriceMinor       int64   `json:"unit_price_minor"`
	DiscountMinor        int64   `json:"discount_minor"`
	TaxableMinor         *int64  `json:"taxable_minor,omitempty"`
	GSTRateBps           *int64  `json:"gst_rate_bps,omitempty"`
	TaxMinor             int64   `json:"tax_minor"`
	LineTotalMinor       int64   `json:"line_total_minor"`
	TaxCode              *string `json:"tax_code,omitempty"`
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Order, error) {
	input.ClientOrderID = strings.TrimSpace(input.ClientOrderID)
	input.StoreID = strings.TrimSpace(input.StoreID)
	if input.ClientOrderID == "" || input.StoreID == "" || len(input.Items) == 0 {
		return Order{}, ErrInvalidOrder
	}
	if input.Currency == "" {
		input.Currency = "INR"
	}

	if existing, err := s.GetByClientID(ctx, input.ClientOrderID); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Order{}, err
	}

	customerID := input.CustomerID.StringPtr()
	if customerID != nil {
		var exists int
		err := s.db.SQL().QueryRowContext(ctx, `SELECT 1 FROM customers WHERE id = ?`, *customerID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return Order{}, ErrCustomerNotFound
		}
		if err != nil {
			return Order{}, fmt.Errorf("validate customer %s: %w", *customerID, err)
		}
	}

	orderID := newID("ord")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var built []Item
	var subtotal, discount, tax, total int64
	var resolvedDiscountPolicy *DiscountPolicy
	var resolvedTaxPolicy *TaxPolicy

	for i, in := range input.Items {
		productID := in.ProductID.String()
		if productID == "" || in.QuantityMilli <= 0 || in.DiscountMinor < 0 || in.TaxMinor < 0 {
			return Order{}, ErrInvalidOrder
		}
		product, err := s.catalog.GetProduct(ctx, productID, input.StoreID)
		if err != nil {
			return Order{}, fmt.Errorf("load product %s: %w", productID, err)
		}

		var categoryIDSnapshot *string
		var categoryNameSnapshot *string
		if product.CategoryID != nil && strings.TrimSpace(*product.CategoryID) != "" {
			categoryID := strings.TrimSpace(*product.CategoryID)
			categoryIDSnapshot = &categoryID
			category, categoryErr := s.catalog.GetCategory(ctx, categoryID)
			if categoryErr == nil {
				categoryName := strings.TrimSpace(category.Name)
				if categoryName != "" {
					categoryNameSnapshot = &categoryName
				}
			} else if !errors.Is(categoryErr, catalog.ErrNotFound) {
				return Order{}, fmt.Errorf("load category %s for product %s: %w", categoryID, productID, categoryErr)
			}
		}

		price := int64(0)
		if in.UnitPriceMinor != nil {
			if *in.UnitPriceMinor < 0 {
				return Order{}, ErrInvalidOrder
			}
			isOverride := product.Price == nil || *in.UnitPriceMinor != product.Price.AmountMinor
			if isOverride {
				allowed := product.AllowManualPrice
				if s.priceOverridePolicy != nil {
					policyAllowed, policyErr := s.priceOverridePolicy(ctx)
					if policyErr != nil {
						return Order{}, fmt.Errorf("%w: price override: %v", ErrPricingPolicyUnavailable, policyErr)
					}
					allowed = allowed && policyAllowed
				}
				if !allowed {
					return Order{}, ErrPriceOverrideNotAllowed
				}
			}
			price = *in.UnitPriceMinor
		} else if product.Price != nil {
			price = product.Price.AmountMinor
		} else {
			return Order{}, fmt.Errorf("product %s has no price", in.ProductID)
		}
		gross := price * in.QuantityMilli / 1000
		if in.DiscountMinor > 0 && s.discountPolicy != nil {
			if resolvedDiscountPolicy == nil {
				policy, policyErr := s.discountPolicy(ctx)
				if policyErr != nil {
					return Order{}, fmt.Errorf("%w: discount: %v", ErrPricingPolicyUnavailable, policyErr)
				}
				if policy.MaxPercent < 0 || policy.MaxPercent > 100 {
					return Order{}, fmt.Errorf("%w: invalid discount max percent %.4f", ErrPricingPolicyUnavailable, policy.MaxPercent)
				}
				resolvedDiscountPolicy = &policy
			}
			if !resolvedDiscountPolicy.Allowed {
				return Order{}, ErrDiscountNotAllowed
			}
			maxDiscount := int64(float64(gross) * resolvedDiscountPolicy.MaxPercent / 100)
			if in.DiscountMinor > maxDiscount {
				return Order{}, ErrDiscountLimitExceeded
			}
		}
		taxable := gross - in.DiscountMinor
		if taxable < 0 {
			return Order{}, ErrInvalidOrder
		}
		lineTax := in.TaxMinor
		lineTotal := taxable + lineTax
		if s.taxPolicy != nil {
			if resolvedTaxPolicy == nil {
				policy, policyErr := s.taxPolicy(ctx)
				if policyErr != nil {
					return Order{}, fmt.Errorf("%w: %v", ErrTaxPolicyUnavailable, policyErr)
				}
				resolvedTaxPolicy = &policy
			}
			lineTax, lineTotal, err = calculateTax(*resolvedTaxPolicy, taxable, product.GSTRateBps)
			if err != nil {
				return Order{}, err
			}
		}
		if lineTotal < 0 {
			return Order{}, ErrInvalidOrder
		}
		barcode := in.Barcode
		if barcode == nil && len(product.Barcodes) > 0 {
			barcode = &product.Barcodes[0]
		}
		built = append(built, Item{
			ID: newID("itm"), LineNo: i + 1, ProductID: product.ID, SKU: product.SKU, ProductName: product.Name,
			Barcode: barcode, CategoryIDSnapshot: categoryIDSnapshot, CategoryNameSnapshot: categoryNameSnapshot,
			QuantityMilli: in.QuantityMilli, UnitPriceMinor: price, DiscountMinor: in.DiscountMinor,
			TaxableMinor: &taxable, GSTRateBps: product.GSTRateBps, TaxMinor: lineTax, LineTotalMinor: lineTotal, TaxCode: product.TaxCode,
		})
		subtotal += gross
		discount += in.DiscountMinor
		tax += lineTax
		total += lineTotal
	}

	order := Order{
		ID: orderID, ClientOrderID: input.ClientOrderID, StoreID: input.StoreID, TerminalID: input.TerminalID,
		CustomerID: customerID, CreatedByUserID: creatorUserIDFromContext(ctx), Status: "confirmed", Currency: input.Currency, SubtotalMinor: subtotal,
		DiscountMinor: discount, TaxMinor: tax, TotalMinor: total, Notes: input.Notes, Version: 1,
		CreatedAt: now, UpdatedAt: now, Items: built,
	}

	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
            INSERT INTO sales_orders(id,client_order_id,store_id,terminal_id,customer_id,created_by_user_id,status,currency,subtotal_minor,discount_minor,tax_minor,total_minor,notes,source,version,created_at,updated_at)
            VALUES(?,?,?,?,?,?,'confirmed',?,?,?,?,?,?,'pos',1,?,?)`,
			order.ID, order.ClientOrderID, order.StoreID, order.TerminalID, order.CustomerID, order.CreatedByUserID, order.Currency,
			order.SubtotalMinor, order.DiscountMinor, order.TaxMinor, order.TotalMinor, order.Notes, now, now,
		); err != nil {
			return err
		}

		for _, item := range order.Items {
			if _, err := tx.ExecContext(ctx, `
                INSERT INTO sales_order_items(id,order_id,line_no,product_id,sku,product_name,barcode,category_id_snapshot,category_name_snapshot,quantity_milli,unit_price_minor,discount_minor,taxable_minor,gst_rate_bps,tax_minor,line_total_minor,tax_code,created_at)
                VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				item.ID, order.ID, item.LineNo, item.ProductID, item.SKU, item.ProductName, item.Barcode,
				item.CategoryIDSnapshot, item.CategoryNameSnapshot, item.QuantityMilli, item.UnitPriceMinor, item.DiscountMinor,
				item.TaxableMinor, item.GSTRateBps, item.TaxMinor, item.LineTotalMinor, item.TaxCode, now,
			); err != nil {
				return err
			}
		}
		return s.saveSnapshot(ctx, tx, order)
	})
	if err != nil {
		return Order{}, fmt.Errorf("create order: %w", err)
	}
	return order, nil
}

func (s *Service) Get(ctx context.Context, id string) (Order, error) {
	return s.getOne(ctx, `WHERE id = ?`, id)
}

func (s *Service) GetByClientID(ctx context.Context, clientID string) (Order, error) {
	return s.getOne(ctx, `WHERE client_order_id = ?`, clientID)
}

func (s *Service) Complete(ctx context.Context, id string) (Order, error) {
	order, err := s.Get(ctx, id)
	if err != nil {
		return Order{}, err
	}
	if order.CompletedAt != nil || order.Status == "paid" || order.Status == "cancelled" || order.Status == "returned" {
		return Order{}, ErrAlreadyComplete
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	order.CompletedAt = &now
	order.UpdatedAt = now
	order.Version++
	if order.Status == "draft" {
		order.Status = "confirmed"
	}
	if err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE sales_orders SET completed_at=?, updated_at=?, version=? WHERE id=?`, now, now, order.Version, id); err != nil {
			return err
		}
		return s.saveSnapshot(ctx, tx, order)
	}); err != nil {
		return Order{}, err
	}
	return order, nil
}

func (s *Service) List(ctx context.Context, storeID, status string, limit, offset int) ([]Order, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	q := `SELECT id FROM sales_orders WHERE store_id = ?`
	args := []any{storeID}
	if status != "" {
		q += ` AND status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.db.SQL().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Order, 0, len(ids))
	for _, id := range ids {
		order, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, order)
	}
	return out, nil
}

func (s *Service) getOne(ctx context.Context, where string, value any) (Order, error) {
	row := s.db.SQL().QueryRowContext(ctx, `
        SELECT id,client_order_id,store_id,terminal_id,customer_id,created_by_user_id,completed_by_user_id,status,currency,subtotal_minor,discount_minor,tax_minor,total_minor,notes,version,completed_at,created_at,updated_at
        FROM sales_orders `+where, value)
	var o Order
	if err := row.Scan(&o.ID, &o.ClientOrderID, &o.StoreID, &o.TerminalID, &o.CustomerID, &o.CreatedByUserID, &o.CompletedByUserID, &o.Status, &o.Currency, &o.SubtotalMinor, &o.DiscountMinor, &o.TaxMinor, &o.TotalMinor, &o.Notes, &o.Version, &o.CompletedAt, &o.CreatedAt, &o.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Order{}, ErrNotFound
		}
		return Order{}, err
	}
	rows, err := s.db.SQL().QueryContext(ctx, `
        SELECT id,line_no,product_id,sku,product_name,barcode,category_id_snapshot,category_name_snapshot,quantity_milli,unit_price_minor,discount_minor,taxable_minor,gst_rate_bps,tax_minor,line_total_minor,tax_code
        FROM sales_order_items WHERE order_id=? ORDER BY line_no`, o.ID)
	if err != nil {
		return Order{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var i Item
		if err := rows.Scan(&i.ID, &i.LineNo, &i.ProductID, &i.SKU, &i.ProductName, &i.Barcode, &i.CategoryIDSnapshot, &i.CategoryNameSnapshot, &i.QuantityMilli, &i.UnitPriceMinor, &i.DiscountMinor, &i.TaxableMinor, &i.GSTRateBps, &i.TaxMinor, &i.LineTotalMinor, &i.TaxCode); err != nil {
			return Order{}, err
		}
		o.Items = append(o.Items, i)
	}
	return o, rows.Err()
}

func (s *Service) saveSnapshot(ctx context.Context, tx *sql.Tx, order Order) error {
	raw, err := json.Marshal(order)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sales_order_snapshots(order_id,version,snapshot_json,created_at) VALUES(?,?,?,?)`,
		order.ID, order.Version, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func newID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UTC().UnixNano())
}
