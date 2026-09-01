package postgresqladapter

import "testing"

func TestRebindSkipsQuotedAndCommentQuestionMarks(t *testing.T) {
	input := "SELECT ?, '?', \"?\", $$?$$, $tag$?$tag$ -- ?\n/* ? */ WHERE id = ?"
	want := "SELECT $1, '?', \"?\", $$?$$, $tag$?$tag$ -- ?\n/* ? */ WHERE id = $2"
	if got := rebind(input); got != want {
		t.Fatalf("rebind mismatch:\nwant: %s\n got: %s", want, got)
	}
}

func TestRebindLeavesPostgreSQLParametersAlone(t *testing.T) {
	input := "SELECT $1, $2"
	if got := rebind(input); got != input {
		t.Fatalf("expected unchanged query, got %q", got)
	}
}
