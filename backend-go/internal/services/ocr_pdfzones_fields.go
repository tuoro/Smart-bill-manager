package services

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type pdfZonesCandidate struct {
	val  string
	src  string
	conf float64
}

func zonesRowText(row PDFTextZonesRow) string {
	if t := strings.TrimSpace(row.Text); t != "" {
		return t
	}
	if len(row.Spans) == 0 {
		return ""
	}
	spans := make([]PDFTextZonesSpan, 0, len(row.Spans))
	spans = append(spans, row.Spans...)
	sort.Slice(spans, func(i, j int) bool { return spans[i].X0 < spans[j].X0 })
	parts := make([]string, 0, len(spans))
	for _, sp := range spans {
		if t := strings.TrimSpace(sp.T); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func zonesRowSpansSorted(row PDFTextZonesRow) []PDFTextZonesSpan {
	if len(row.Spans) == 0 {
		return nil
	}
	spans := make([]PDFTextZonesSpan, 0, len(row.Spans))
	spans = append(spans, row.Spans...)
	sort.Slice(spans, func(i, j int) bool { return spans[i].X0 < spans[j].X0 })
	return spans
}

func zonesCleanupPartyName(s string) string {
	s = cleanupName(strings.TrimSpace(s))
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, "/\\|,.;:：，。；")
	return strings.TrimSpace(s)
}

func zonesExtractValueToRightOfLabel(row PDFTextZonesRow, label string, stopLabels []string) string {
	label = strings.TrimSpace(label)
	if label == "" || len(row.Spans) == 0 {
		return ""
	}
	spans := zonesRowSpansSorted(row)

	labelIdx := -1
	for i, sp := range spans {
		if strings.Contains(sp.T, label) {
			labelIdx = i
			break
		}
	}
	if labelIdx == -1 {
		return ""
	}

	labelRight := spans[labelIdx].X1
	nextLabelLeft := math.Inf(1)
	for i := labelIdx + 1; i < len(spans); i++ {
		t := strings.TrimSpace(spans[i].T)
		if t == "" {
			continue
		}
		if strings.Contains(t, "\u540d\u79f0") || strings.Contains(t, "\u7eb3\u7a0e\u4eba\u8bc6\u522b\u53f7") || strings.Contains(t, "\u7edf\u4e00\u793e\u4f1a\u4fe1\u7528\u4ee3\u7801") ||
			strings.Contains(t, "\u5730\u5740") || strings.Contains(t, "\u7535\u8bdd") || strings.Contains(t, "\u5f00\u6237\u884c") || strings.Contains(t, "\u8d26\u53f7") ||
			strings.Contains(t, "\u9879\u76ee\u540d\u79f0") || strings.Contains(t, "\u89c4\u683c\u578b\u53f7") || strings.Contains(t, "\u5355\u4f4d") || strings.Contains(t, "\u6570\u91cf") {
			nextLabelLeft = spans[i].X0
			break
		}
		for _, stop := range stopLabels {
			if stop != "" && strings.Contains(t, stop) {
				nextLabelLeft = spans[i].X0
				break
			}
		}
		if !math.IsInf(nextLabelLeft, 1) {
			break
		}
	}

	parts := make([]string, 0, 8)
	for i := labelIdx + 1; i < len(spans); i++ {
		sp := spans[i]
		t := strings.TrimSpace(sp.T)
		if t == "" {
			continue
		}
		if sp.X0 < labelRight-2 {
			continue
		}
		if sp.X0 >= nextLabelLeft {
			break
		}
		parts = append(parts, t)
	}

	return zonesCleanupPartyName(strings.Join(parts, " "))
}

func extractBuyerNameFromPDFZones(pages []PDFTextZonesPage) (pdfZonesCandidate, bool) {
	best := pdfZonesCandidate{}
	bestScore := -1

	stopLabels := []string{
		"\u7eb3\u7a0e\u4eba\u8bc6\u522b\u53f7",
		"\u7edf\u4e00\u793e\u4f1a\u4fe1\u7528\u4ee3\u7801",
		"\u5730\u5740",
		"\u7535\u8bdd",
		"\u5f00\u6237\u884c",
		"\u5f00\u6237\u884c\u53ca\u8d26\u53f7",
		"\u5f00\u6237\u884c\u53ca\u5e10\u53f7",
		"\u8d26\u53f7",
		"\u9879\u76ee\u540d\u79f0",
		"\u89c4\u683c\u578b\u53f7",
		"\u5355\u4f4d",
		"\u6570\u91cf",
	}

	for _, p := range pages {
		h := p.Height
		if h <= 0 {
			h = 1
		}
		for _, row := range p.Rows {
			region := strings.ToLower(strings.TrimSpace(row.Region))
			if region != "buyer" && region != "header_left" && region != "password" {
				continue
			}

			rowText := zonesRowText(row)
			if strings.TrimSpace(rowText) == "" {
				continue
			}

			cands := make([]pdfZonesCandidate, 0, 4)

			// Strong patterns: explicit "购买方名称:".
			if v := zonesExtractValueToRightOfLabel(row, "\u8d2d\u4e70\u65b9\u540d\u79f0", stopLabels); v != "" && !isBadPartyNameCandidate(v) {
				cands = append(cands, pdfZonesCandidate{val: v, src: "pymupdf_zones_buyer_name_label", conf: 0.92})
			}
			// Common VAT table label: "名称:" in the buyer block.
			if v := zonesExtractValueToRightOfLabel(row, "\u540d\u79f0", stopLabels); v != "" && !isBadPartyNameCandidate(v) {
				cands = append(cands, pdfZonesCandidate{val: v, src: "pymupdf_zones_buyer_name", conf: 0.9})
			}
			// Some PDFs glue the buyer's name into the "开户行及账号" value; accept only person-like suffixes.
			if v := zonesExtractValueToRightOfLabel(row, "\u5f00\u6237\u884c\u53ca\u8d26\u53f7", stopLabels); v != "" {
				if strings.HasSuffix(v, "\u5148\u751f") || strings.HasSuffix(v, "\u5973\u58eb") {
					v = zonesCleanupPartyName(v)
					if v != "" && !isBadPartyNameCandidate(v) {
						cands = append(cands, pdfZonesCandidate{val: v, src: "pymupdf_zones_buyer_bank_field_name", conf: 0.78})
					}
				}
			}

			// Company name near tax-id label (mostly for enterprise buyers).
			if taxIDRegex.MatchString(rowText) {
				if v := extractNameFromTaxIDLabelLine(rowText); v != "" && !isBadPartyNameCandidate(v) {
					cands = append(cands, pdfZonesCandidate{val: v, src: "pymupdf_zones_buyer_taxid_label", conf: 0.85})
				}
			}

			// Fallback: preserve "个人" as a valid buyer name if it appears in the buyer block.
			if strings.Contains(rowText, "\u4e2a\u4eba") {
				cands = append(cands, pdfZonesCandidate{val: "\u4e2a\u4eba", src: "pymupdf_zones_buyer_personal", conf: 0.7})
			}

			for _, cand := range cands {
				v := zonesCleanupPartyName(cand.val)
				if v == "" || isBadPartyNameCandidate(v) {
					continue
				}
				if v != "\u4e2a\u4eba" && len([]rune(v)) < 2 {
					continue
				}

				score := len([]rune(v))
				switch region {
				case "buyer", "header_left":
					score += 30
				case "password":
					score += 6
				}
				yRatio := row.Y0 / h
				if yRatio >= 0.18 && yRatio <= 0.48 {
					score += 8
				}
				if cand.src == "pymupdf_zones_buyer_name_label" || cand.src == "pymupdf_zones_buyer_name" {
					score += 50
				}
				if cand.src == "pymupdf_zones_buyer_taxid_label" {
					score += 35
				}
				if score > bestScore {
					best = pdfZonesCandidate{val: v, src: cand.src, conf: cand.conf}
					bestScore = score
				}
			}
		}
	}

	return best, strings.TrimSpace(best.val) != ""
}

type pdfZonesMoneyCandidate struct {
	v float64
	x float64
}

var pdfZonesDecimalRegex = regexp.MustCompile(`\d+(?:,\d{3})*\.\d{1,2}`)

func zonesExtractMoneyFromRow(row PDFTextZonesRow) []pdfZonesMoneyCandidate {
	spans := zonesRowSpansSorted(row)
	if len(spans) == 0 {
		return nil
	}
	out := make([]pdfZonesMoneyCandidate, 0, 6)
	for _, sp := range spans {
		t := strings.TrimSpace(sp.T)
		if t == "" {
			continue
		}
		m := pdfZonesDecimalRegex.FindString(t)
		if m == "" {
			continue
		}
		m = strings.ReplaceAll(m, ",", "")
		f, err := strconv.ParseFloat(m, 64)
		if err != nil {
			continue
		}
		out = append(out, pdfZonesMoneyCandidate{v: f, x: sp.X0})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].x < out[j].x })
	return out
}

func extractInvoiceTotalsFromPDFZones(pages []PDFTextZonesPage) (total *float64, totalSrc string, totalConf float64, tax *float64, taxSrc string, taxConf float64) {
	bestTotalScore := -1
	bestTaxScore := -1

	for _, p := range pages {
		if p.Page != 1 {
			continue
		}
		for _, row := range p.Rows {
			rowText := zonesRowText(row)
			if rowText == "" {
				continue
			}
			region := strings.ToLower(strings.TrimSpace(row.Region))

			cands := zonesExtractMoneyFromRow(row)
			if len(cands) == 0 {
				continue
			}

			hasTotals := strings.Contains(rowText, "\u4ef7\u7a0e\u5408\u8ba1")
			hasXiaoxie := strings.Contains(rowText, "\u5c0f\u5199")
			hasHeji := strings.Contains(rowText, "\u5408\u8ba1")
			hasTaxLabel := strings.Contains(rowText, "\u7a0e\u989d")

			regionBoost := 0
			switch region {
			case "items", "seller", "footer", "remarks":
				regionBoost = 8
			}

			if hasTotals {
				// Prefer the amount immediately to the right of "(小写)" when present.
				xiaoxieX := -1.0
				for _, sp := range row.Spans {
					if strings.Contains(sp.T, "\u5c0f\u5199") {
						xiaoxieX = sp.X0
						break
					}
				}
				if xiaoxieX >= 0 {
					for _, c := range cands {
						if c.x > xiaoxieX {
							v := c.v
							score := 120 + regionBoost
							if score > bestTotalScore {
								total = &v
								totalSrc = "pymupdf_zones_total_xiaoxie"
								totalConf = 0.95
								bestTotalScore = score
							}
							break
						}
					}
				} else {
					// Some PDFs don't surface the "(小写)" token as a span; fall back to the max amount on the row.
					maxV := cands[0].v
					for _, c := range cands[1:] {
						if c.v > maxV {
							maxV = c.v
						}
					}
					v := maxV
					score := 105 + regionBoost
					if score > bestTotalScore {
						total = &v
						totalSrc = "pymupdf_zones_total_row_max"
						totalConf = 0.9
						bestTotalScore = score
					}
				}
				// Tax: prefer the right-most amount on the totals row (often "... (小写) ￥X ￥税额").
				if len(cands) >= 2 {
					v := cands[len(cands)-1].v
					score := 110 + regionBoost
					if hasTaxLabel {
						score += 10
					}
					if score > bestTaxScore {
						tax = &v
						taxSrc = "pymupdf_zones_tax_totals_row"
						taxConf = 0.92
						bestTaxScore = score
					}
				}
				continue
			}

			// "合计 ￥net ￥tax" row: compute total=net+tax and take tax directly.
			if hasHeji && len(cands) >= 2 && len(cands) <= 3 && !hasXiaoxie {
				net := cands[0].v
				taxV := cands[1].v
				if taxV > 0 && net > 0 {
					sum := net + taxV
					sumScore := 70 + regionBoost
					if sumScore > bestTotalScore {
						total = &sum
						totalSrc = "pymupdf_zones_total_net_plus_tax"
						totalConf = 0.78
						bestTotalScore = sumScore
					}
					taxScore := 80 + regionBoost
					if taxScore > bestTaxScore {
						tax = &taxV
						taxSrc = "pymupdf_zones_tax_heji_row"
						taxConf = 0.82
						bestTaxScore = taxScore
					}
				}
				continue
			}

			// Dedicated "税额" rows (often in the password block).
			if hasTaxLabel {
				v := cands[len(cands)-1].v
				score := 55
				if region == "password" {
					score += 10
				}
				if score > bestTaxScore {
					tax = &v
					taxSrc = "pymupdf_zones_tax_label_row"
					taxConf = 0.75
					bestTaxScore = score
				}
				continue
			}
		}
	}

	return total, totalSrc, totalConf, tax, taxSrc, taxConf
}
