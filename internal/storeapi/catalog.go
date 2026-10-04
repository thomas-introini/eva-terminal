package storeapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// GetProducts fetches Store API products.
func (c *Client) GetProducts(ctx context.Context, q ProductQuery) ([]Product, error) {
	query := url.Values{}
	if q.Page > 0 {
		query.Set("page", strconv.Itoa(q.Page))
	}
	if q.PerPage > 0 {
		query.Set("per_page", strconv.Itoa(q.PerPage))
	}
	if q.Search != "" {
		query.Set("search", q.Search)
	}
	if q.InStock {
		query.Set("stock_status", "instock")
	}
	if q.Type != "" {
		query.Set("type", q.Type)
	}
	if len(q.ParentIDs) > 0 {
		var ids []string
		for _, id := range q.ParentIDs {
			ids = append(ids, strconv.Itoa(id))
		}
		query.Set("parent", strings.Join(ids, ","))
	}

	var result []Product
	if err := c.doJSON(ctx, http.MethodGet, "/products", query, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetProduct fetches a single product.
func (c *Client) GetProduct(ctx context.Context, id int) (*Product, error) {
	var result Product
	if err := c.doJSON(ctx, http.MethodGet, "/products/"+strconv.Itoa(id), nil, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetProductVariations fetches variation rows via product query.
func (c *Client) GetProductVariations(ctx context.Context, parentID int) ([]Product, error) {
	return c.GetProducts(ctx, ProductQuery{
		Type:      "variation",
		ParentIDs: []int{parentID},
		PerPage:   100,
	})
}

// GetProductCategories fetches categories.
func (c *Client) GetProductCategories(ctx context.Context) ([]ProductCategory, error) {
	var result []ProductCategory
	if err := c.doJSON(ctx, http.MethodGet, "/products/categories", nil, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetProductAttributes fetches product attributes.
func (c *Client) GetProductAttributes(ctx context.Context) ([]ProductAttribute, error) {
	var result []ProductAttribute
	if err := c.doJSON(ctx, http.MethodGet, "/products/attributes", nil, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetProductAttributeTerms fetches terms by attribute id.
func (c *Client) GetProductAttributeTerms(ctx context.Context, attributeID int) ([]Term, error) {
	var result []Term
	path := "/products/attributes/" + strconv.Itoa(attributeID) + "/terms"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}
