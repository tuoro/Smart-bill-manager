package dingtalk

import "net/http"

// 只认这四种，与 documents 表的 CHECK 一致；且 declared_mime 必须等于探测结果，
// 所以不能信任钉钉给的文件名后缀，只能看字节。
func sniffMIME(content []byte) string {
	switch detected := http.DetectContentType(content); detected {
	case "image/jpeg", "image/png", "image/webp", "application/pdf":
		return detected
	default:
		return ""
	}
}
