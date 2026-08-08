package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HASAN-SHAIK/SHAJRetailProducts-POSService/internal/database"
)

var ErrNotFound = errors.New("catalog item not found")

type Product struct {
	ID               string   `json:"id"`
	CategoryID       *string  `json:"category_id,omitempty"`
	SKU              *string  `json:"sku,omitempty"`
	Name             string   `json:"name"`
	Description      *string  `json:"description,omitempty"`
	UnitOfMeasure    *string  `json:"unit_of_measure,omitempty"`
	TaxCode          *string  `json:"tax_code,omitempty"`
	IsActive         bool     `json:"is_active"`
	AllowManualPrice bool     `json:"allow_manual_price"`
	TrackInventory   bool     `json:"track_inventory"`
	Barcodes         []string `json:"barcodes"`
	Price            *Price   `json:"price,omitempty"`
}

type Price struct {
	Currency     string `json:"currency"`
	AmountMinor  int64  `json:"amount_minor"`
	TaxInclusive bool   `json:"tax_inclusive"`
}

type Category struct {
	ID        string  `json:"id"`
	ParentID  *string `json:"parent_id,omitempty"`
	Name      string  `json:"name"`
	Code      *string `json:"code,omitempty"`
	SortOrder int     `json:"sort_order"`
}

type Repository struct {
	db *database.DB
}

func NewRepository(db *database.DB) *Repository { return &Repository{db: db} }

func (r *Repository) GetProduct(ctx context.Context, id, storeID string) (Product, error) {
	row := r.db.SQL().QueryRowContext(ctx, `
        SELECT id, category_id, sku, name, description, unit_of_measure, tax_code,
               is_active, allow_manual_price, track_inventory
        FROM catalog_products
        WHERE id = ?`, id)
	p, err := scanProduct(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Product{}, ErrNotFound
		}
		return Product{}, fmt.Errorf("get catalog product: %w", err)
	}
	if err := r.attachBarcodesAndPrice(ctx, &p, storeID); err != nil {
		return Product{}, err
	}
	return p, nil
}

func (r *Repository) GetByBarcode(ctx context.Context, barcode, storeID string) (Product, error) {
	barcode = strings.TrimSpace(barcode)
	if barcode == "" {
		return Product{}, ErrNotFound
	}
	row := r.db.SQL().QueryRowContext(ctx, `
        SELECT p.id, p.category_id, p.sku, p.name, p.description, p.unit_of_measure, p.tax_code,
               p.is_active, p.allow_manual_price, p.track_inventory
        FROM catalog_products p
        JOIN catalog_barcodes b ON b.product_id = p.id
        WHERE b.barcode = ? AND p.is_active = 1`, barcode)
	p, err := scanProduct(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Product{}, ErrNotFound
		}
		return Product{}, fmt.Errorf("get product by barcode: %w", err)
	}
	if err := r.attachBarcodesAndPrice(ctx, &p, storeID); err != nil {
		return Product{}, err
	}
	return p, nil
}

func (r *Repository) Search(ctx context.Context, query, storeID string, limit int) ([]Product, error) {
	query = strings.TrimSpace(query)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	like := "%" + query + "%"
	rows, err := r.db.SQL().QueryContext(ctx, `
        SELECT DISTINCT p.id, p.category_id, p.sku, p.name, p.description, p.unit_of_measure, p.tax_code,
               p.is_active, p.allow_manual_price, p.track_inventory
        FROM catalog_products p
        LEFT JOIN catalog_barcodes b ON b.product_id = p.id
        WHERE p.is_active = 1
          AND (? = '' OR p.name LIKE ? COLLATE NOCASE OR p.sku LIKE ? COLLATE NOCASE OR b.barcode LIKE ?)
        ORDER BY p.name
        LIMIT ?`, query, like, like, like, limit)
	if err != nil {
		return nil, fmt.Errorf("search catalog: %w", err)
	}
	defer rows.Close()
	var products []Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range products {
		if err := r.attachBarcodesAndPrice(ctx, &products[i], storeID); err != nil {
			return nil, err
		}
	}
	return products, nil
}

func (r *Repository) ListCategories(ctx context.Context) ([]Category, error) {
	rows, err := r.db.SQL().QueryContext(ctx, `
        SELECT id, parent_id, name, code, sort_order
        FROM catalog_categories WHERE is_active = 1
        ORDER BY sort_order, name`)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.ParentID, &c.Name, &c.Code, &c.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) attachBarcodesAndPrice(ctx context.Context, p *Product, storeID string) error {
	rows, err := r.db.SQL().QueryContext(ctx, `SELECT barcode FROM catalog_barcodes WHERE product_id = ? ORDER BY is_primary DESC, barcode`, p.ID)
	if err != nil {
		return fmt.Errorf("load barcodes: %w", err)
	}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			rows.Close()
			return err
		}
		p.Barcodes = append(p.Barcodes, code)
	}
	if err := rows.Close(); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	var price Price
	var taxInclusive int
	err = r.db.SQL().QueryRowContext(ctx, `
        SELECT currency, amount_minor, tax_inclusive
        FROM catalog_prices
        WHERE product_id = ?
          AND (store_id IS NULL OR store_id = ?)
          AND (valid_from IS NULL OR valid_from <= ?)
          AND (valid_to IS NULL OR valid_to > ?)
        ORDER BY CASE WHEN store_id = ? THEN 0 ELSE 1 END, priority DESC, updated_at DESC
        LIMIT 1`, p.ID, storeID, now, now, storeID).Scan(&price.Currency, &price.AmountMinor, &taxInclusive)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("load price: %w", err)
	}
	if err == nil {
		price.TaxInclusive = taxInclusive == 1
		p.Price = &price
	}
	return nil
}

type scanner interface{ Scan(dest ...any) error }

func scanProduct(s scanner) (Product, error) {
	var p Product
	var active, manual, inventory int
	if err := s.Scan(&p.ID, &p.CategoryID, &p.SKU, &p.Name, &p.Description, &p.UnitOfMeasure, &p.TaxCode, &active, &manual, &inventory); err != nil {
		return Product{}, err
	}
	p.IsActive = active == 1
	p.AllowManualPrice = manual == 1
	p.TrackInventory = inventory == 1
	return p, nil
}
