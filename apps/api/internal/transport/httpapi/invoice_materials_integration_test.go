package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func httpMaterialMultipart(t *testing.T, fields [][2]string, count int) (*bytes.Buffer, string) {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for _, field := range fields {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < count; index++ {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="file"; filename="synthetic-aux.png"`)
		header.Set("Content-Type", "image/png")
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(syntheticPNG(t, color.RGBA{R: 63, G: 32, B: 19, A: 255})); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &buffer, writer.FormDataContentType()
}

func seedHTTPMaterialInvoice(t *testing.T, f *httpTestFixture, owner *testSession) string {
	t.Helper()
	upload := f.upload(t, owner, "synthetic-invoice-source.png", "image/png", syntheticPNG(t, color.RGBA{R: 15, G: 34, B: 65, A: 255}))
	f.processNext(t)
	job := decodeMap(t, f.request(http.MethodGet, "/api/v1/jobs/"+upload["job_id"], nil, owner, false, ""))
	path := "/api/v1/jobs/" + upload["job_id"] + "/manual-review"
	body := fmt.Sprintf(`{"expected_job_version":%v,"document_type":"invoice","reason":"合成发票核对"}`, job["version"])
	assertStatus(t, f.requestWithHeaders(http.MethodPost, path, strings.NewReader(body), owner, true, "application/json", map[string]string{"Idempotency-Key": "http-material-root"}), http.StatusOK)
	reviewPath := "/api/v1/reviews/" + upload["job_id"]
	root := decodeMap(t, f.request(http.MethodGet, reviewPath, nil, owner, false, ""))
	values := map[string]any{"invoice_number": "SYN-MATERIAL-HTTP", "invoice_date": "2026-08-27", "seller_name": "合成销售方", "buyer_name": "合成购买方", "total_minor": 10000, "currency": "CNY"}
	fields := []map[string]any{}
	for _, raw := range root["fields"].([]any) {
		field := raw.(map[string]any)
		name := asString(t, field["path"])
		if name == "document_type" {
			continue
		}
		entry := map[string]any{"path": name, "value_type": field["value_type"], "presence": "absent"}
		if value, exists := values[name]; exists {
			entry["presence"] = "present"
			entry["value"] = value
			entry["manual_evidence"] = []map[string]any{{"page": 1, "quote": "合成发票摘录"}}
		}
		fields = append(fields, entry)
	}
	payload, err := json.Marshal(map[string]any{"expected_revision": root["revision"], "expected_optimistic_version": root["optimistic_version"], "document_type": "invoice", "fields": fields})
	if err != nil {
		t.Fatal(err)
	}
	revised := f.request(http.MethodPost, reviewPath+"/revisions", bytes.NewReader(payload), owner, true, "application/json")
	assertStatus(t, revised, http.StatusCreated)
	confirmed := f.requestWithHeaders(http.MethodPost, reviewPath+"/confirm", bytes.NewReader(httpConfirmPayload(t, decodeMap(t, revised), true)), owner, true, "application/json", map[string]string{"Idempotency-Key": "http-material-confirm"})
	assertStatus(t, confirmed, http.StatusOK)
	return asString(t, decodeMap(t, confirmed)["fact_id"])
}

func TestHTTPInvoiceMaterialsWorkflowAndClosedBoundary(t *testing.T) {
	f := newHTTPTestFixture(t)
	defer f.store.Close()
	owner := f.login(t, f.owner.TenantID)
	finance, reviewer, viewer := f.addRoleSession(t, domain.RoleFinance), f.addRoleSession(t, domain.RoleReviewer), f.addRoleSession(t, domain.RoleViewer)
	id := seedHTTPMaterialInvoice(t, f, owner)
	path := "/api/v1/invoices/" + id + "/materials"
	w := f.request(http.MethodGet, path, nil, finance, false, "")
	assertStatus(t, w, http.StatusOK)
	version := fmt.Sprint(decodeMap(t, w)["version"])
	fields := [][2]string{{"expected_version", version}, {"reason", "合成上传材料"}, {"idempotency_key", "http-material-upload"}}
	for _, denied := range []*testSession{reviewer, viewer} {
		assertStatus(t, f.request(http.MethodGet, path, nil, denied, false, ""), http.StatusForbidden)
		buffer, mime := httpMaterialMultipart(t, fields, 1)
		assertStatus(t, f.request(http.MethodPost, path+"/upload", buffer, denied, true, mime), http.StatusForbidden)
	}
	buffer, mime := httpMaterialMultipart(t, fields, 1)
	assertStatus(t, f.request(http.MethodPost, path+"/upload", buffer, finance, false, mime), http.StatusForbidden)
	for _, test := range []struct {
		fields [][2]string
		count  int
	}{{fields, 2}, {fields, 0}, {append(append([][2]string{}, fields...), [2]string{"reason", "duplicate"}), 1}, {append(append([][2]string{}, fields...), [2]string{"other", "unknown"}), 1}, {fields[:2], 1}} {
		buffer, mime := httpMaterialMultipart(t, test.fields, test.count)
		assertStatus(t, f.request(http.MethodPost, path+"/upload", buffer, finance, true, mime), http.StatusBadRequest)
	}
	buffer, mime = httpMaterialMultipart(t, fields, 1)
	created := f.request(http.MethodPost, path+"/upload", buffer, finance, true, mime)
	assertStatus(t, created, http.StatusOK)
	result := decodeMap(t, created)
	buffer, mime = httpMaterialMultipart(t, fields, 1)
	replay := f.request(http.MethodPost, path+"/upload", buffer, finance, true, mime)
	assertStatus(t, replay, http.StatusOK)
	if decodeMap(t, replay)["replayed"] != true {
		t.Fatal("HTTP upload replay missing")
	}
	documentID, linkID := asString(t, result["document_id"]), asString(t, result["link_id"])
	for _, session := range []*testSession{owner, finance} {
		assertStatus(t, f.request(http.MethodGet, "/api/v1/documents/"+documentID+"/content", nil, session, false, ""), http.StatusOK)
	}
	assertStatus(t, f.request(http.MethodGet, "/api/v1/documents/"+documentID+"/content", nil, reviewer, false, ""), http.StatusNotFound)
	assertStatus(t, f.request(http.MethodGet, "/api/v1/documents/"+documentID+"/content", nil, viewer, false, ""), http.StatusForbidden)
	for _, query := range []string{"limit=0", "limit=101", "limit=x", "q=a&q=b", "q=%zz", "other=x", "cursor=bad"} {
		assertStatus(t, f.request(http.MethodGet, "/api/v1/invoices/"+id+"/material-candidates?"+query, nil, owner, false, ""), http.StatusBadRequest)
	}
	removePath := path + "/" + linkID + "/remove"
	valid := fmt.Sprintf(`{"expected_version":%v,"reason":"合成解除","idempotency_key":"http-material-remove"}`, result["version"])
	for _, invalid := range []string{valid[:len(valid)-1] + `,"reason":"重复"}`, valid[:len(valid)-1] + `,"other":true}`, `null`, valid + `{}`, `{"reason":"missing"}`, strings.Replace(valid, `"expected_version":`, `"Expected_version":`, 1)} {
		assertStatus(t, f.request(http.MethodPost, removePath, strings.NewReader(invalid), owner, true, "application/json"), http.StatusBadRequest)
	}
	assertStatus(t, f.request(http.MethodPost, removePath, strings.NewReader(valid), owner, true, "application/json"), http.StatusOK)
	assertStatus(t, f.request(http.MethodPost, removePath, strings.NewReader(valid), owner, true, "application/json"), http.StatusOK)
	latest := f.request(http.MethodGet, path, nil, owner, false, "")
	assertStatus(t, latest, http.StatusOK)
	if len(decodeMap(t, latest)["items"].([]any)) != 0 {
		t.Fatal("HTTP material not removed")
	}
	var jobs int
	if err := f.store.DB().QueryRow(`SELECT count(*) FROM processing_jobs WHERE document_id=?`, documentID).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatal("auxiliary HTTP created job")
	}
}
