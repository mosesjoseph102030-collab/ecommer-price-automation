package woocommerce

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"automation/internal/db"
)

// LivePrice is the current price state read directly from WooCommerce.
type LivePrice struct {
	ProductExternalID  string `json:"product_external_id"`
	VariantExternalID  string `json:"variant_external_id,omitempty"`
	RegularPriceKobo   int64  `json:"regular_price_kobo"`
	SalePriceKobo      *int64 `json:"sale_price_kobo,omitempty"`
	EffectivePriceKobo int64  `json:"effective_price_kobo"`
	StockStatus        string `json:"stock_status"`
	DateModified       string `json:"date_modified"`
	HasSalePrice       bool   `json:"has_sale_price"`
}

// ErrProductGone means the mapped WooCommerce product or variant no longer exists.
var ErrProductGone = errors.New("product no longer exists in WooCommerce; remap the product before retrying")

// ErrSalePriceConflict means a sale price is active, so a regular-price-only
// publish would silently change the customer-visible selling price.
var ErrSalePriceConflict = errors.New("product has an active sale price; resolve the sale price before publishing")

// LoadProductLive resolves the tenant's mapped product/variant and reads the
// current price from WooCommerce. It never trusts the stored catalog price.
func (s *Service) LoadProductLive(ctx context.Context, orgID, productID, variantID string) (LivePrice, error) {
	var storeID, productExternal, variantExternal string
	err := dbTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT store_id, external_id FROM products WHERE organization_id=$1 AND id=$2`, orgID, productID).
			Scan(&storeID, &productExternal); err != nil {
			return err
		}
		if variantID != "" {
			if err := tx.QueryRowContext(ctx, `SELECT external_id FROM product_variants WHERE organization_id=$1 AND id=$2 AND product_id=$3`, orgID, variantID, productID).
				Scan(&variantExternal); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return LivePrice{}, err
	}
	if storeID == "" || productExternal == "" {
		return LivePrice{}, errors.New("product is not mapped to a WooCommerce product")
	}
	_, _, client, err := s.loadForStore(ctx, orgID, storeID)
	if err != nil {
		return LivePrice{}, err
	}
	productExternalInt, err := strconv.ParseInt(productExternal, 10, 64)
	if err != nil {
		return LivePrice{}, errors.New("product mapping is invalid")
	}
	if variantExternal == "" {
		product, err := client.GetProduct(ctx, productExternalInt)
		if err != nil {
			if IsNotFound(err) {
				return LivePrice{}, ErrProductGone
			}
			return LivePrice{}, err
		}
		return livePriceFromProduct(productExternal, "", product)
	}
	variantExternalInt, err := strconv.ParseInt(variantExternal, 10, 64)
	if err != nil {
		return LivePrice{}, errors.New("variant mapping is invalid")
	}
	variant, err := client.GetVariation(ctx, productExternalInt, variantExternalInt)
	if err != nil {
		if IsNotFound(err) {
			return LivePrice{}, ErrProductGone
		}
		return LivePrice{}, err
	}
	return livePriceFromVariant(productExternal, variantExternal, variant)
}

// PublishRegularPrice writes one regular price and returns the verified result.
// The caller must re-read with LoadProductLive to confirm; this function returns
// the server response for immediate comparison.
func (s *Service) PublishRegularPrice(ctx context.Context, orgID, productID, variantID string, priceKobo int64) (LivePrice, error) {
	var storeID, productExternal, variantExternal string
	err := dbTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT store_id, external_id FROM products WHERE organization_id=$1 AND id=$2`, orgID, productID).
			Scan(&storeID, &productExternal); err != nil {
			return err
		}
		if variantID != "" {
			if err := tx.QueryRowContext(ctx, `SELECT external_id FROM product_variants WHERE organization_id=$1 AND id=$2 AND product_id=$3`, orgID, variantID, productID).
				Scan(&variantExternal); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return LivePrice{}, err
	}
	_, _, client, err := s.loadForStore(ctx, orgID, storeID)
	if err != nil {
		return LivePrice{}, err
	}
	productExternalInt, err := strconv.ParseInt(productExternal, 10, 64)
	if err != nil {
		return LivePrice{}, errors.New("product mapping is invalid")
	}
	if variantExternal == "" {
		product, err := client.UpdateRegularPrice(ctx, productExternalInt, priceKobo)
		if err != nil {
			if IsNotFound(err) {
				return LivePrice{}, ErrProductGone
			}
			return LivePrice{}, err
		}
		return livePriceFromProduct(productExternal, "", product)
	}
	variantExternalInt, err := strconv.ParseInt(variantExternal, 10, 64)
	if err != nil {
		return LivePrice{}, errors.New("variant mapping is invalid")
	}
	variant, err := client.UpdateVariationRegularPrice(ctx, productExternalInt, variantExternalInt, priceKobo)
	if err != nil {
		if IsNotFound(err) {
			return LivePrice{}, ErrProductGone
		}
		return LivePrice{}, err
	}
	return livePriceFromVariant(productExternal, variantExternal, variant)
}

func (s *Service) loadForStore(ctx context.Context, orgID, storeID string) (*Connection, *Credentials, *Client, error) {
	var connectionID string
	err := dbTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT id FROM store_connections WHERE organization_id=$1 AND store_id=$2 ORDER BY updated_at DESC LIMIT 1`, orgID, storeID).Scan(&connectionID)
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return s.load(ctx, orgID, connectionID)
}

func livePriceFromProduct(externalID, variantExternalID string, p Product) (LivePrice, error) {
	raw := p.RegularPrice
	if raw == "" {
		raw = p.Price
	}
	regular, err := MoneyToKobo(raw)
	if err != nil {
		return LivePrice{}, err
	}
	var sale *int64
	hasSale := strings.TrimSpace(p.SalePrice) != ""
	if hasSale {
		v, err := MoneyToKobo(p.SalePrice)
		if err != nil {
			return LivePrice{}, err
		}
		sale = &v
	}
	effective := regular
	if sale != nil {
		effective = *sale
	}
	return LivePrice{ProductExternalID: externalID, VariantExternalID: variantExternalID, RegularPriceKobo: regular,
		SalePriceKobo: sale, EffectivePriceKobo: effective, StockStatus: p.StockStatus, DateModified: p.DateModified, HasSalePrice: hasSale}, nil
}

func livePriceFromVariant(externalID, variantExternalID string, v Variant) (LivePrice, error) {
	raw := v.RegularPrice
	if raw == "" {
		raw = v.Price
	}
	regular, err := MoneyToKobo(raw)
	if err != nil {
		return LivePrice{}, err
	}
	var sale *int64
	hasSale := strings.TrimSpace(v.SalePrice) != ""
	if hasSale {
		value, err := MoneyToKobo(v.SalePrice)
		if err != nil {
			return LivePrice{}, err
		}
		sale = &value
	}
	effective := regular
	if sale != nil {
		effective = *sale
	}
	return LivePrice{ProductExternalID: externalID, VariantExternalID: variantExternalID, RegularPriceKobo: regular,
		SalePriceKobo: sale, EffectivePriceKobo: effective, StockStatus: v.StockStatus, DateModified: v.DateModified, HasSalePrice: hasSale}, nil
}

func dbTenant(ctx context.Context, database *sql.DB, orgID string, fn func(*sql.Tx) error) error {
	return db.WithTenant(ctx, database, orgID, fn)
}
