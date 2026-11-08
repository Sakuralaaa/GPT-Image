package service

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"chatgpt2api/internal/util"
)

// AccountExportColumns is the column order of the credential CSV export.
var AccountExportColumns = []string{
	"email", "password", "mail_provider", "mail_ref", "mail_token", "mail_channel",
	"access_token", "session_token", "refresh_token",
	"type", "status", "enabled", "note", "created_at", "last_refresh_at", "last_error",
}

// ExportAccountsByIDs exports the given accounts (all when ids is empty).
func (s *AccountService) ExportAccountsByIDs(ids []string) []map[string]any {
	all := s.ExportAccounts()
	if len(ids) == 0 {
		return all
	}
	wanted := map[string]struct{}{}
	for _, id := range ids {
		wanted[strings.TrimSpace(id)] = struct{}{}
	}
	out := []map[string]any{}
	for _, item := range all {
		if _, ok := wanted[util.Clean(item["id"])]; ok {
			out = append(out, item)
		}
	}
	return out
}

// AccountsToCSV renders exported accounts as CSV with a header row.
func AccountsToCSV(items []map[string]any) (string, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(AccountExportColumns); err != nil {
		return "", err
	}
	for _, item := range items {
		row := make([]string, len(AccountExportColumns))
		for i, col := range AccountExportColumns {
			row[i] = util.Clean(item[col])
		}
		if err := w.Write(row); err != nil {
			return "", err
		}
	}
	w.Flush()
	return buf.String(), w.Error()
}

// ParseAccountTransfer parses an exported JSON array / {items:[...]} or CSV
// document back into account records.
func ParseAccountTransfer(format, content string) ([]map[string]any, error) {
	content = strings.TrimSpace(strings.TrimPrefix(content, "\ufeff"))
	if content == "" {
		return nil, errors.New("内容为空")
	}
	if format == "" {
		if strings.HasPrefix(content, "[") || strings.HasPrefix(content, "{") {
			format = "json"
		} else {
			format = "csv"
		}
	}
	switch strings.ToLower(format) {
	case "json":
		var raw any
		if err := json.Unmarshal([]byte(content), &raw); err != nil {
			return nil, fmt.Errorf("JSON 解析失败: %w", err)
		}
		items := util.AsMapSlice(raw)
		if len(items) == 0 {
			items = util.AsMapSlice(util.StringMap(raw)["items"])
		}
		if len(items) == 0 {
			items = util.AsMapSlice(util.StringMap(raw)["accounts"])
		}
		if len(items) == 0 {
			return nil, errors.New("JSON 中没有账号记录（需要数组或 {items:[...]}）")
		}
		return items, nil
	case "csv":
		r := csv.NewReader(strings.NewReader(content))
		r.FieldsPerRecord = -1
		rows, err := r.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("CSV 解析失败: %w", err)
		}
		if len(rows) < 2 {
			return nil, errors.New("CSV 至少需要表头和一行数据")
		}
		header := make([]string, len(rows[0]))
		hasEmail := false
		for i, h := range rows[0] {
			header[i] = strings.ToLower(strings.TrimSpace(h))
			if header[i] == "email" {
				hasEmail = true
			}
		}
		if !hasEmail {
			return nil, errors.New("CSV 表头缺少 email 列")
		}
		items := []map[string]any{}
		for _, row := range rows[1:] {
			item := map[string]any{}
			for i, v := range row {
				if i < len(header) && header[i] != "" {
					item[header[i]] = strings.TrimSpace(v)
				}
			}
			items = append(items, item)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("不支持的格式 %s", format)
	}
}

// accountImportFields are copied from an import record onto the account.
var accountImportFields = []string{"email", "password", "mail_provider", "mail_ref", "mail_token", "mail_channel", "session_token", "refresh_token", "note", "type"}

// ImportAccountCredentials upserts records by email: existing accounts get
// their credentials (and tokens, when different) updated; unknown emails are
// added when the record carries an access_token.
func (s *AccountService) ImportAccountCredentials(items []map[string]any) map[string]any {
	added, updated, skipped := 0, 0, 0
	errs := []map[string]string{}
	seen := map[string]struct{}{}
	for index, item := range items {
		email := strings.ToLower(util.Clean(item["email"]))
		accessToken := util.Clean(item["access_token"])
		row := fmt.Sprintf("#%d", index+1)
		if email == "" && accessToken == "" {
			skipped++
			errs = append(errs, map[string]string{"row": row, "error": "缺少 email 和 access_token"})
			continue
		}
		if email != "" && !strings.Contains(email, "@") {
			skipped++
			errs = append(errs, map[string]string{"row": row, "email": email, "error": "email 格式无效"})
			continue
		}
		key := firstNonEmpty(email, accessToken)
		if _, dup := seen[key]; dup {
			skipped++
			errs = append(errs, map[string]string{"row": row, "email": email, "error": "文件内重复"})
			continue
		}
		seen[key] = struct{}{}
		updates := map[string]any{}
		for _, field := range accountImportFields {
			if v := util.Clean(item[field]); v != "" {
				updates[field] = v
			}
		}
		if email != "" {
			updates["email"] = email
		}
		if v, ok := item["enabled"]; ok && util.Clean(v) != "" {
			updates["enabled"] = util.ToBool(v)
		}
		existing := ""
		if email != "" {
			existing = s.AccountTokenByEmail(email)
		}
		if existing == "" && accessToken != "" && s.GetAccount(accessToken) != nil {
			existing = accessToken
		}
		if existing != "" {
			target := existing
			if accessToken != "" && accessToken != existing {
				if s.RefreshAccountViaSession(existing, accessToken, util.Clean(firstNonEmpty(util.Clean(updates["session_token"]), util.Clean(s.AccountCredentials(existing)["session_token"]))), "") {
					target = accessToken
				}
			}
			if s.UpdateAccount(target, updates) == nil {
				skipped++
				errs = append(errs, map[string]string{"row": row, "email": email, "error": "更新失败"})
				continue
			}
			updated++
			continue
		}
		if accessToken == "" {
			skipped++
			errs = append(errs, map[string]string{"row": row, "email": email, "error": "号池中不存在且缺少 access_token（可导入后再通过找回任务登录）"})
			continue
		}
		s.AddAccounts([]string{accessToken})
		if len(updates) > 0 {
			s.UpdateAccount(accessToken, updates)
		}
		added++
	}
	if added+updated > 0 {
		s.logs.Add(fmt.Sprintf("导入账号凭证：新增 %d，更新 %d，跳过 %d", added, updated, skipped), map[string]any{
			"module": "accounts", "operation_type": "导入", "added": added, "updated": updated, "skipped": skipped,
		})
	}
	return map[string]any{"added": added, "updated": updated, "skipped": skipped, "errors": errs}
}
