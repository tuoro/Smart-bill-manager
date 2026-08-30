package domain

import (
	"encoding/json"
	"sort"
)

type ClaimReviewPage struct {
	PageNumber int
	FieldPaths []string
	ItemKeys   []string
}

type InvoiceItemPageSpan struct {
	ItemKey     string
	SortOrder   int
	PageNumbers []int
	StartPage   int
	EndPage     int
	CrossPage   bool
}

type ClaimPagePlan struct {
	Pages            []ClaimReviewPage
	InvoiceItemSpans []InvoiceItemPageSpan
}

type invoiceItemPageAccumulator struct {
	key          string
	hasPresent   bool
	sortOrder    int
	hasSortOrder bool
	pages        map[int]struct{}
}

// BuildClaimPagePlan derives a disposable review projection from the current
// Claim fields and their Evidence. It never persists or repairs page meaning.
func BuildClaimPagePlan(documentType DocumentType, fields []FieldCandidate, pageCount int) ClaimPagePlan {
	plan, _ := analyzeClaimPagePlan(documentType, fields, pageCount)
	return plan
}

func analyzeClaimPagePlan(
	documentType DocumentType,
	fields []FieldCandidate,
	pageCount int,
) (ClaimPagePlan, []ClaimValidation) {
	plan := ClaimPagePlan{}
	pageFields := make([]map[string]struct{}, pageCount)
	for page := 1; page <= pageCount; page++ {
		plan.Pages = append(plan.Pages, ClaimReviewPage{PageNumber: page, FieldPaths: []string{}, ItemKeys: []string{}})
		pageFields[page-1] = make(map[string]struct{})
	}
	items := make(map[string]*invoiceItemPageAccumulator)
	for _, field := range fields {
		if field.Presence != "present" {
			continue
		}
		itemKey, property, isItem := splitStableItemPath(field.Path)
		if isItem {
			item := items[itemKey]
			if item == nil {
				item = &invoiceItemPageAccumulator{key: itemKey, pages: make(map[int]struct{})}
				items[itemKey] = item
			}
			item.hasPresent = true
			if property == "sort_order" {
				value, err := rawInteger(field.Value)
				if err == nil && value >= 0 && value <= int64(^uint(0)>>1) {
					item.sortOrder = int(value)
					item.hasSortOrder = true
				}
			}
		}
		for _, evidence := range field.Evidence {
			if evidence.Page < 1 || evidence.Page > pageCount {
				continue
			}
			pageFields[evidence.Page-1][field.Path] = struct{}{}
			if isItem && property != "sort_order" {
				items[itemKey].pages[evidence.Page] = struct{}{}
			}
		}
	}

	var validations []ClaimValidation
	if documentType == DocumentInvoice {
		validations = validateAndAppendInvoiceItemSpans(&plan, items)
	}
	for index := range plan.Pages {
		for path := range pageFields[index] {
			plan.Pages[index].FieldPaths = append(plan.Pages[index].FieldPaths, path)
		}
		sort.Strings(plan.Pages[index].FieldPaths)
		for _, span := range plan.InvoiceItemSpans {
			if containsPage(span.PageNumbers, plan.Pages[index].PageNumber) {
				plan.Pages[index].ItemKeys = append(plan.Pages[index].ItemKeys, span.ItemKey)
			}
		}
	}
	return plan, validations
}

func validateAndAppendInvoiceItemSpans(
	plan *ClaimPagePlan,
	items map[string]*invoiceItemPageAccumulator,
) []ClaimValidation {
	active := make([]*invoiceItemPageAccumulator, 0, len(items))
	for _, item := range items {
		if item.hasPresent {
			active = append(active, item)
		}
	}
	sort.Slice(active, func(left, right int) bool {
		if active[left].hasSortOrder != active[right].hasSortOrder {
			return active[left].hasSortOrder
		}
		if active[left].sortOrder != active[right].sortOrder {
			return active[left].sortOrder < active[right].sortOrder
		}
		return active[left].key < active[right].key
	})

	var validations []ClaimValidation
	seenOrders := make(map[int]string, len(active))
	for _, item := range active {
		path := "items[" + item.key + "].sort_order"
		if !item.hasSortOrder {
			validations = append(validations, pagePlanBlocked(path, "invoice_item_sort_order_invalid", "发票明细顺序必须是可用的非负整数"))
			continue
		}
		if _, duplicate := seenOrders[item.sortOrder]; duplicate {
			validations = append(validations, pagePlanBlocked(path, "invoice_item_sort_order_duplicate", "发票明细顺序不能重复"))
		}
		seenOrders[item.sortOrder] = item.key
		if item.sortOrder >= len(active) {
			validations = append(validations, pagePlanBlocked(path, "invoice_item_sort_order_gap", "发票明细顺序必须从 0 连续排列"))
		}
	}
	for expected := 0; expected < len(active); expected++ {
		if _, exists := seenOrders[expected]; !exists {
			validations = append(validations, pagePlanBlocked("", "invoice_item_sort_order_gap", "发票明细顺序必须从 0 连续排列"))
			break
		}
	}

	previousEnd := 0
	for _, item := range active {
		pages := sortedPages(item.pages)
		span := InvoiceItemPageSpan{
			ItemKey: item.key, SortOrder: item.sortOrder, PageNumbers: pages,
		}
		if len(pages) != 0 {
			span.StartPage = pages[0]
			span.EndPage = pages[len(pages)-1]
			span.CrossPage = len(pages) > 1
		}
		plan.InvoiceItemSpans = append(plan.InvoiceItemSpans, span)
		if len(pages) == 0 {
			validations = append(validations, pagePlanBlocked("items["+item.key+"]", "invoice_item_page_unresolved", "发票明细没有可定位的页面证据"))
			continue
		}
		for index := 1; index < len(pages); index++ {
			if pages[index] != pages[index-1]+1 {
				validations = append(validations, pagePlanBlocked("items["+item.key+"]", "invoice_item_page_gap", "跨页发票明细的证据页必须连续"))
				break
			}
		}
		if previousEnd != 0 && span.StartPage < previousEnd {
			validations = append(validations, pagePlanBlocked("items["+item.key+"].sort_order", "invoice_item_page_order_conflict", "发票明细阅读顺序与页面顺序冲突"))
		}
		if span.EndPage > previousEnd {
			previousEnd = span.EndPage
		}
	}
	return uniquePagePlanValidations(validations)
}

func splitStableItemPath(path string) (string, string, bool) {
	match := stableItemPath.FindStringSubmatch(path)
	if match == nil {
		return "", "", false
	}
	return match[1], match[2], true
}

func sortedPages(values map[int]struct{}) []int {
	result := make([]int, 0, len(values))
	for page := range values {
		result = append(result, page)
	}
	sort.Ints(result)
	return result
}

func containsPage(pages []int, page int) bool {
	index := sort.SearchInts(pages, page)
	return index < len(pages) && pages[index] == page
}

func pagePlanBlocked(path, code, message string) ClaimValidation {
	return ClaimValidation{FieldPath: path, RuleCode: code, Severity: "blocked", Status: "blocked", SafeMessage: message}
}

func uniquePagePlanValidations(values []ClaimValidation) []ClaimValidation {
	seen := make(map[string]struct{}, len(values))
	result := make([]ClaimValidation, 0, len(values))
	for _, value := range values {
		encoded, _ := json.Marshal([]string{value.FieldPath, value.RuleCode})
		key := string(encoded)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}
