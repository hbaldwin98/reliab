package effects

import (
	"path"
	"strings"
)

var (
	// writeVerbs write, move, or delete the files they name, with
	// PowerShell's built-in aliases for them (del, ri, copy, sc, and the
	// rest; about_Aliases). sed and perl write only with -i; git only in
	// the subcommands gitWrites names. It is a heuristic; the CI gate is the
	// backstop.
	writeVerbs = map[string]bool{
		"cp": true, "mv": true, "rm": true, "rmdir": true, "tee": true, "truncate": true, "dd": true, "install": true,
		"ln": true, "touch": true, "chmod": true,
		"set-content": true, "add-content": true, "out-file": true, "remove-item": true, "copy-item": true, "move-item": true,
		"new-item": true, "rename-item": true, "clear-content": true, "del": true, "erase": true, "ri": true, "rd": true,
		"copy": true, "cpi": true, "move": true, "mi": true, "sc": true, "ac": true, "ni": true, "ren": true, "rni": true, "clc": true,
	}
	// dirVerbs rewrite or remove a whole directory's files when given one,
	// so a directory holding an oracle is protected too. Other write verbs
	// only write into a directory they name.
	dirVerbs     = map[string]bool{"rm": true, "rmdir": true, "remove-item": true, "del": true, "erase": true, "ri": true, "rd": true}
	gitWrites    = map[string]bool{"checkout": true, "restore": true, "rm": true, "mv": true, "clean": true, "reset": true}
	gitDirWrites = map[string]bool{"checkout": true, "restore": true, "rm": true, "clean": true, "reset": true}
	// gitValueOptions are git's global options that take the next word
	// (git(1) SYNOPSIS); -C also moves where relative paths resolve
	// (dirFlags).
	gitValueOptions = map[string]bool{"-c": true, "-C": true, "--git-dir": true, "--work-tree": true, "--namespace": true, "--exec-path": true, "--config-env": true}
	// outputFlags name a file or directory a command writes.
	outputFlags = map[string]bool{"--output": true, "--output-file": true, "--output-directory": true, "-outfile": true}
	// shortOutputs are commands whose short option names the file they
	// write. git's -o names one only in format-patch, which sets it.
	shortOutputs = map[string]string{"curl": "-o", "sort": "-o", "go": "-o", "wget": "-O"}
	// headKeywords may stand before the command a simple command runs.
	headKeywords = map[string]bool{"time": true, "if": true, "then": true, "else": true, "elif": true, "while": true, "until": true, "do": true, "!": true, "{": true, "}": true}
	// wrappers run the command after their options, and list the options
	// that take a value.
	wrappers = map[string]map[string]bool{
		"sudo":    set("-u", "-g", "-h", "-p", "-r", "-t", "-C", "-D", "-U", "-T", "--user", "--group"),
		"doas":    set("-u", "-C"),
		"env":     set("-u", "-C", "-S", "--unset", "--chdir", "--split-string"),
		"nice":    set("-n", "--adjustment"),
		"ionice":  set("-c", "-n", "-p", "-P", "-u", "--class", "--classdata"),
		"timeout": set("-s", "-k", "--signal", "--kill-after"),
		"nohup":   {}, "time": set("-f", "-o", "--format", "--output"), "command": {}, "exec": set("-a"), "builtin": {},
		"xargs":   set("-a", "-d", "-E", "-e", "-I", "-i", "-L", "-l", "-n", "-P", "-s", "--arg-file", "--delimiter", "--max-args", "--max-procs", "--max-chars", "--replace", "--max-lines"),
		"stdbuf":  set("-i", "-o", "-e", "--input", "--output", "--error"),
		"busybox": {}, "&": {}, "chronic": {}, "unbuffer": {},
	}
	// shells run the word after -c, and their standard input, as code.
	shells = set("sh", "bash", "zsh", "dash", "ksh", "mksh", "ash", "fish", "su", "runuser")
	// pwshes run the words after -Command, and their standard input, as
	// PowerShell code.
	pwshes = set("pwsh", "powershell", "pwsh-preview")
)

func set(xs ...string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// name returns the lower-cased base name of a command word, without .exe.
func name(w string) string {
	w = strings.ToLower(strings.ReplaceAll(w, `\`, "/"))
	return strings.TrimSuffix(path.Base(w), ".exe")
}

// head returns the index of the word a simple command runs, past
// assignments, keywords, and wrappers with their options, or -1.
func head(ws []string) int {
	for i := 0; i < len(ws); {
		w := ws[i]
		if assign.MatchString(w) || headKeywords[strings.ToLower(w)] {
			i++
			continue
		}
		n := name(w)
		opts, ok := wrappers[n]
		if !ok {
			return i
		}
		for i++; i < len(ws) && strings.HasPrefix(ws[i], "-") && len(ws[i]) > 1; i++ {
			if ws[i] == "--" {
				i++
				break
			}
			if opts[ws[i]] {
				i++
			}
		}
		if n == "env" {
			for i < len(ws) && assign.MatchString(ws[i]) {
				i++
			}
		}
		if n == "timeout" && i < len(ws) {
			i++
		}
	}
	return -1
}

// gitSubcommand returns the index in args, a git command's words after
// git, of its subcommand, past the global options; -1 when there is none.
func gitSubcommand(args []string) int {
	for k := 0; k < len(args); k++ {
		a := args[k]
		if gitValueOptions[a] {
			k++
		} else if !strings.HasPrefix(a, "-") {
			return k
		}
	}
	return -1
}

// operands returns the words a write verb may write: every word after it,
// and the value of a PowerShell parameter written -Name:value.
func operands(args []string, sh Shell) []string {
	out := append([]string{}, args...)
	if sh == PowerShell {
		for _, a := range args {
			if p, v, ok := strings.Cut(a, ":"); ok && strings.HasPrefix(p, "-") && v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

// effectsOf returns what one simple command may write and remove whole, and
// whether it pushes. code is the text it runs as code, read as a line of
// its own in shell codeSh; cmds are commands it runs whose words it names
// (find -exec).
func effectsOf(ws []string, sh Shell) (writes, whole []string, push bool, code []nestedCode, cmds [][]string) {
	writes = outputs(ws)
	h := head(ws)
	if h < 0 {
		return
	}
	n, args := name(ws[h]), ws[h+1:]
	if n == "git" {
		k := gitSubcommand(args)
		for j := 0; j+1 < len(args) && (k < 0 || j < k); j++ {
			// git -c alias.x='!cmd' and git -c core.pager=cmd run cmd.
			if args[j] == "-c" {
				key, val, _ := strings.Cut(args[j+1], "=")
				key = strings.ToLower(key)
				if strings.HasPrefix(key, "alias.") && strings.HasPrefix(val, "!") {
					code = append(code, nestedCode{val[1:], Bash})
				} else if key == "core.pager" || key == "core.editor" || key == "core.sshcommand" || key == "sequence.editor" {
					code = append(code, nestedCode{val, Bash})
				}
			}
		}
		if k < 0 {
			return
		}
		sub, rest := strings.ToLower(args[k]), args[k+1:]
		if gitWrites[sub] {
			writes = append(writes, rest...)
			if gitDirWrites[sub] {
				whole = append(whole, rest...)
			}
		}
		push = sub == "push"
		if sub == "rebase" {
			for j, a := range rest {
				if (a == "-x" || a == "--exec") && j+1 < len(rest) {
					code = append(code, nestedCode{rest[j+1], Bash})
				} else if v, ok := strings.CutPrefix(a, "--exec="); ok {
					code = append(code, nestedCode{v, Bash})
				}
			}
		}
	} else if n == "gh" {
		var pos []string
		for _, a := range args {
			if !strings.HasPrefix(a, "-") {
				pos = append(pos, a)
			}
		}
		push = len(pos) >= 2 && pos[0] == "pr" && pos[1] == "create"
	} else if n == "sed" || n == "perl" {
		for _, a := range args {
			if strings.HasPrefix(a, "--in-place") && n == "sed" || len(a) > 1 && a[0] == '-' && a[1] != '-' && inPlace(a[1:]) {
				writes = append(writes, args...)
				break
			}
		}
	} else if n == "dd" {
		for _, a := range args {
			if v, ok := strings.CutPrefix(a, "of="); ok {
				writes = append(writes, v)
			}
		}
	} else if n == "find" {
		writes, whole, cmds = find(args, writes)
	} else if shells[n] {
		dashC := false
		for j, a := range args {
			if a == "--" {
				break
			}
			if a == "-c" || a == "--command" || len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsRune(a[1:], 'c') {
				dashC = true
				continue
			}
			if dashC && !strings.HasPrefix(a, "-") {
				code = append(code, nestedCode{args[j], Bash})
				break
			}
		}
	} else if pwshes[n] {
		for j, a := range args {
			low := strings.ToLower(a)
			if len(low) >= 2 && (strings.HasPrefix("-command", low) || strings.HasPrefix("/command", low)) {
				code = append(code, nestedCode{strings.Join(args[j+1:], " "), PowerShell})
				break
			}
		}
	} else if n == "cmd" {
		for j, a := range args {
			if low := strings.ToLower(a); low == "/c" || low == "/k" {
				code = append(code, nestedCode{strings.Join(args[j+1:], " "), sh})
				break
			}
		}
	} else if n == "eval" || n == "watch" {
		code = append(code, nestedCode{strings.Join(args, " "), sh})
	} else if n == "iex" || n == "invoke-expression" {
		var rest []string
		for _, a := range args {
			if !strings.EqualFold(a, "-command") {
				rest = append(rest, a)
			}
		}
		code = append(code, nestedCode{strings.Join(rest, " "), PowerShell})
	} else if writeVerbs[n] {
		ops := operands(args, sh)
		writes = append(writes, ops...)
		if dirVerbs[n] {
			whole = append(whole, ops...)
		}
	}
	return
}

// inPlace reports whether a cluster of short options holds -i, as sed -i,
// sed -ni, and perl -pi do.
func inPlace(cluster string) bool {
	for i := 0; i < len(cluster); i++ {
		c := cluster[i]
		if c == 'i' {
			return true
		}
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return false
}

// find returns what find(1) may write: with -delete, or -exec (and
// -execdir, -ok, -okdir) running a write verb, the paths it starts from,
// whole; and the commands -exec runs.
func find(args, writes []string) ([]string, []string, [][]string) {
	var roots []string
	k := 0
	for ; k < len(args); k++ {
		a := args[k]
		if strings.HasPrefix(a, "-") || a == "(" || a == "!" {
			break
		}
		roots = append(roots, a)
	}
	var whole []string
	var cmds [][]string
	deletes := false
	for ; k < len(args); k++ {
		a := args[k]
		if a == "-delete" {
			deletes = true
		}
		if a != "-exec" && a != "-execdir" && a != "-ok" && a != "-okdir" {
			continue
		}
		end := k + 1
		for end < len(args) && args[end] != ";" && args[end] != "+" {
			end++
		}
		cmd := args[k+1 : end]
		cmds = append(cmds, cmd)
		if h := head(cmd); h >= 0 && (writeVerbs[name(cmd[h])] || name(cmd[h]) == "sed" || name(cmd[h]) == "perl") {
			writes = append(writes, roots...)
			if dirVerbs[name(cmd[h])] {
				deletes = true
			}
		}
		k = end
	}
	if deletes {
		writes = append(writes, roots...)
		whole = append(whole, roots...)
	}
	return writes, whole, cmds
}

// readsCode reports whether a command runs its standard input as code: a
// shell or PowerShell given no script, or Invoke-Expression given nothing.
func readsCode(ws []string) bool {
	h := head(ws)
	if h < 0 {
		return false
	}
	n, args := name(ws[h]), ws[h+1:]
	if n == "iex" || n == "invoke-expression" {
		return len(args) == 0
	}
	if !shells[n] && !pwshes[n] {
		return false
	}
	for _, a := range args {
		if a == "-" || a == "-s" {
			return true
		}
		if !strings.HasPrefix(a, "-") {
			return false
		}
	}
	return true
}

// outputs returns the files a simple command's options name for output:
// --output, --output-file, or --output-directory (git diff and log write
// a patch with --output), PowerShell's -OutFile (also -OutFile:<path>),
// and the short option of a command that writes there (shortOutputs):
// git format-patch -o <dir>, curl -o, sort -o, go build -o, wget -O, with
// the value attached or not.
func outputs(ws []string) []string {
	var out []string
	short := ""
	for k, w := range ws {
		low := strings.ToLower(w)
		if o, ok := shortOutputs[strings.TrimSuffix(path.Base(strings.ReplaceAll(low, `\`, "/")), ".exe")]; ok && short == "" {
			short = o
		}
		if low == "format-patch" {
			short = "-o"
		}
		n, val, eq := strings.Cut(low, "=")
		if !eq && strings.HasPrefix(low, "-outfile:") {
			n, val, eq = "-outfile", low[len("-outfile:"):], true
		}
		// An if chain, not a switch: Go coverage does not instrument case
		// expressions, so gremlins never ran a mutant there.
		if outputFlags[n] && eq {
			out = append(out, w[len(w)-len(val):])
		} else if outputFlags[n] || short != "" && w == short {
			if k+1 < len(ws) {
				out = append(out, ws[k+1])
			}
		} else if short != "" && strings.HasPrefix(w, short) {
			out = append(out, w[len(short):])
		}
	}
	return out
}
