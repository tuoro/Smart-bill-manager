package regressionfixtures

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestGenerateMatchesCommittedFixturesByteForByte(t *testing.T) {
	generatedRoot := t.TempDir()
	if err := Generate(generatedRoot); err != nil {
		t.Fatalf("生成纯合成回归样本失败: %v", err)
	}

	committedRoot := filepath.Join("..", "..", "..", "internal", "services", "testdata", "regression")
	generated := readFixtureTree(t, generatedRoot)
	committed := readFixtureTree(t, committedRoot)
	if len(generated) != len(fixedFixtures) {
		t.Fatalf("生成文件数为 %d，预期 %d", len(generated), len(fixedFixtures))
	}
	if len(committed) != len(generated) {
		t.Fatalf("已提交文件数为 %d，生成文件数为 %d", len(committed), len(generated))
	}
	for path, want := range generated {
		got, ok := committed[path]
		if !ok {
			t.Fatalf("已提交样本缺少生成路径 %q", path)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("已提交样本与固定生成结果不一致: %q", path)
		}
	}
}

func TestFixedFixturesAreExplicitlySyntheticAndCoverRequiredScenarios(t *testing.T) {
	wantScenarios := map[string]bool{
		ScenarioPayment: false, ScenarioBasic: false, ScenarioMultiItem: false,
		ScenarioAirTicket: false, ScenarioRailTicket: false,
	}
	for _, fixture := range fixedFixtures {
		if !fixture.doc.Synthetic || fixture.doc.Provenance != "SYNTHETIC_FIXED_CONSTANTS" {
			t.Fatalf("样本 %q 缺少固定纯合成来源标记", fixture.path)
		}
		if !strings.Contains(fixture.doc.RawText, SyntheticMarker) || !strings.HasPrefix(fixture.doc.Name, "synthetic_") {
			t.Fatalf("样本 %q 缺少可见纯合成标记", fixture.path)
		}
		if _, ok := wantScenarios[fixture.doc.Scenario]; !ok {
			t.Fatalf("出现未声明场景 %q", fixture.doc.Scenario)
		}
		wantScenarios[fixture.doc.Scenario] = true
	}
	for scenario, found := range wantScenarios {
		if !found {
			t.Fatalf("缺少必需纯合成场景 %q", scenario)
		}
	}
}

func readFixtureTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	paths := make([]string, 0, len(fixedFixtures))
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			paths = append(paths, filepath.ToSlash(relative))
		}
		return nil
	}); err != nil {
		t.Fatalf("读取样本目录失败: %v", err)
	}
	sort.Strings(paths)
	result := make(map[string][]byte, len(paths))
	for _, relative := range paths {
		payload, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatalf("读取样本 %q 失败: %v", relative, err)
		}
		var doc fixtureDocument
		if err := json.Unmarshal(payload, &doc); err != nil {
			t.Fatalf("解析样本 %q 失败: %v", relative, err)
		}
		result[relative] = payload
	}
	return result
}
