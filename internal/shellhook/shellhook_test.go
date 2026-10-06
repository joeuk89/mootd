package shellhook

import "testing"

const block = "# >>> mootd >>>\n[[ $- == *i* ]] && [[ -x \"/opt/homebrew/bin/mootd\" ]] && \"/opt/homebrew/bin/mootd\"\n# <<< mootd <<<\n"

func TestBlock(t *testing.T) {
	if got := Block("/opt/homebrew/bin/mootd"); got != block {
		t.Errorf("got:\n%s", got)
	}
}

func TestInsertAndRemove(t *testing.T) {
	p10k := "# Enable Powerlevel10k instant prompt. Should stay close to the top of ~/.zshrc.\n" +
		"# Initialization code that may require console input must go above this block.\n" +
		"if [[ -r \"${XDG_CACHE_HOME:-$HOME/.cache}/p10k-instant-prompt-${(%):-%n}.zsh\" ]]; then\n" +
		"  source \"${XDG_CACHE_HOME:-$HOME/.cache}/p10k-instant-prompt-${(%):-%n}.zsh\"\n" +
		"fi\n\nexport ZSH=\"$HOME/.oh-my-zsh\"\n"

	tests := []struct {
		name      string
		before    string
		after     string
		aboveP10k bool
	}{
		{"empty file", "", block, false},
		{"plain file", "export A=1\n", "export A=1\n\n" + block, false},
		{"no trailing newline", "export A=1", "export A=1\n\n" + block, false},
		{"powerlevel10k at the top", p10k, block + "\n" + p10k, true},
		{"powerlevel10k below other lines", "export A=1\n\n" + p10k, "export A=1\n\n" + block + "\n" + p10k, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, above := Insert(tt.before, block)
			if got != tt.after || above != tt.aboveP10k {
				t.Fatalf("Insert gave (above=%v):\n%s\nwant:\n%s", above, got, tt.after)
			}
			if !Installed(got) || Installed(tt.before) {
				t.Error("Installed is wrong")
			}

			restored, removed := Remove(got)
			want := tt.before
			if want != "" && want[len(want)-1] != '\n' {
				want += "\n"
			}
			if !removed || restored != want {
				t.Errorf("Remove gave:\n%q\nwant:\n%q", restored, want)
			}
		})
	}
}

func TestRemoveWithoutABlock(t *testing.T) {
	if got, removed := Remove("export A=1\n"); removed || got != "export A=1\n" {
		t.Errorf("got %q, removed=%v", got, removed)
	}
}
