package pricing

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"automation/internal/db"
)

type ImportIssue struct {
	Row     int    `json:"row"`
	SKU     string `json:"sku,omitempty"`
	Message string `json:"message"`
}

type ImportResult struct {
	Imported int           `json:"imported"`
	Failed   int           `json:"failed"`
	Issues   []ImportIssue `json:"issues"`
}

func (s *Service) ImportCostsCSV(ctx context.Context, orgID, actor string, source io.Reader) (ImportResult, error) {
	reader := csv.NewReader(source)
	reader.TrimLeadingSpace = true
	reader.ReuseRecord = false
	header, err := reader.Read()
	if err != nil {
		return ImportResult{}, errors.New("CSV is empty or invalid")
	}
	columns := map[string]int{}
	for i, value := range header {
		columns[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "\ufeff")))] = i
	}
	required := []string{"sku", "supplier_cost_kobo"}
	for _, name := range required {
		if _, ok := columns[name]; !ok {
			return ImportResult{}, fmt.Errorf("missing required CSV column %q", name)
		}
	}
	result := ImportResult{Issues: []ImportIssue{}}
	seen := map[string]int{}
	for rowNum := 2; ; rowNum++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			result.Failed++
			result.Issues = append(result.Issues, ImportIssue{Row: rowNum, Message: "malformed CSV row"})
			continue
		}
		if rowNum > 5001 {
			return result, errors.New("CSV exceeds 5,000 rows")
		}
		get := func(name string) string {
			index, ok := columns[name]
			if !ok || index >= len(record) {
				return ""
			}
			return strings.TrimSpace(record[index])
		}
		sku := get("sku")
		if sku == "" {
			result.Failed++
			result.Issues = append(result.Issues, ImportIssue{Row: rowNum, Message: "SKU is required"})
			continue
		}
		if previous, ok := seen[sku]; ok {
			result.Failed++
			result.Issues = append(result.Issues, ImportIssue{Row: rowNum, SKU: sku, Message: fmt.Sprintf("duplicate SKU; first seen on row %d", previous)})
			continue
		}
		seen[sku] = rowNum
		components := CostComponents{}
		fields := []struct {
			name   string
			target *int64
		}{{"supplier_cost_kobo", &components.SupplierKobo}, {"shipping_cost_kobo", &components.ShippingKobo}, {"packaging_cost_kobo", &components.PackagingKobo}, {"payment_fee_kobo", &components.PaymentFeeKobo}, {"tax_import_cost_kobo", &components.TaxImportKobo}, {"other_allocated_cost_kobo", &components.OtherKobo}}
		valid := true
		for _, field := range fields {
			raw := get(field.name)
			if raw == "" {
				continue
			}
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || value < 0 {
				result.Failed++
				result.Issues = append(result.Issues, ImportIssue{Row: rowNum, SKU: sku, Message: "invalid " + field.name})
				valid = false
				break
			}
			*field.target = value
		}
		if !valid {
			continue
		}
		productID, variantID, err := s.resolveSKU(ctx, orgID, sku)
		if err != nil {
			result.Failed++
			result.Issues = append(result.Issues, ImportIssue{Row: rowNum, SKU: sku, Message: err.Error()})
			continue
		}
		_, err = s.SetCost(ctx, orgID, actor, CostInput{ProductID: productID, VariantID: variantID, Currency: "NGN", Cost: components})
		if err != nil {
			result.Failed++
			result.Issues = append(result.Issues, ImportIssue{Row: rowNum, SKU: sku, Message: err.Error()})
			continue
		}
		result.Imported++
	}
	return result, nil
}

func (s *Service) resolveSKU(ctx context.Context, orgID, sku string) (productID, variantID string, err error) {
	err = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		err = tx.QueryRowContext(ctx, `SELECT id FROM products WHERE organization_id=$1 AND sku=$2 AND status<>'deleted' ORDER BY created_at LIMIT 1`, orgID, sku).Scan(&productID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		rows, queryErr := tx.QueryContext(ctx, `SELECT v.id,v.product_id FROM product_variants v JOIN products p ON p.id=v.product_id WHERE v.organization_id=$1 AND v.sku=$2 AND p.status<>'deleted'`, orgID, sku)
		if queryErr != nil {
			return queryErr
		}
		defer rows.Close()
		type match struct{ id, product string }
		matches := []match{}
		for rows.Next() {
			var value match
			if scanErr := rows.Scan(&value.id, &value.product); scanErr != nil {
				return scanErr
			}
			matches = append(matches, value)
		}
		if queryErr = rows.Err(); queryErr != nil {
			return queryErr
		}
		if len(matches) == 0 {
			return errors.New("SKU not found")
		}
		if len(matches) > 1 {
			return errors.New("SKU is ambiguous across variants")
		}
		variantID, productID = matches[0].id, matches[0].product
		return nil
	})
	return
}
