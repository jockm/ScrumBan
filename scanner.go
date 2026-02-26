package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var columnDirRe = regexp.MustCompile(`^(\d{2})\.(.+)$`)

// frontMatter is what we unmarshal from YAML.
type frontMatter struct {
	Assignee string `yaml:"assignee"`
	Status   string `yaml:"status"`
	Priority string `yaml:"priority"`
}

// Scan reads dataDir and returns the full board state.
func Scan(dataDir string) (*Board, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read data directory: %w", err)
	}

	var warnings []Warning
	// Track which column numbers we have seen to detect duplicates.
	seenNumbers := map[int]string{} // number -> DirName of first seen
	var columns []*Column

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m := columnDirRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue // not a ##.Name folder, ignore
		}
		num, _ := strconv.Atoi(m[1])
		colName := m[2]
		dirName := e.Name()

		if first, dup := seenNumbers[num]; dup {
			warnings = append(warnings, Warning{
				Path:    filepath.Join(dataDir, dirName),
				Message: fmt.Sprintf("duplicate column number %02d: keeping %q, ignoring %q", num, first, dirName),
			})
			continue
		}
		seenNumbers[num] = dirName

		col := &Column{
			Number:  num,
			Name:    colName,
			DirName: dirName,
		}

		// Scan .md files inside this column directory.
		colPath := filepath.Join(dataDir, dirName)
		mdEntries, err := os.ReadDir(colPath)
		if err != nil {
			warnings = append(warnings, Warning{
				Path:    colPath,
				Message: fmt.Sprintf("cannot read column directory: %v", err),
			})
		} else {
			for _, me := range mdEntries {
				if me.IsDir() {
					continue
				}
				if !strings.HasSuffix(me.Name(), ".md") {
					continue
				}
				storyPath := filepath.Join(colPath, me.Name())
				story, warn := parseStory(storyPath, dirName, me.Name())
				if warn != nil {
					warnings = append(warnings, *warn)
					continue
				}
				col.Stories = append(col.Stories, story)
			}
		}

		columns = append(columns, col)
	}

	// Sort columns by number.
	sort.Slice(columns, func(i, j int) bool {
		return columns[i].Number < columns[j].Number
	})

	return &Board{
		Columns:  columns,
		Warnings: warnings,
	}, nil
}

// parseStory parses a single .md file. Returns a warning and nil story if the
// file cannot be used.
func parseStory(path, columnDir, filename string) (*Story, *Warning) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &Warning{Path: path, Message: fmt.Sprintf("cannot read file: %v", err)}
	}

	content := string(data)

	// Front matter must be the very first thing in the file.
	if !strings.HasPrefix(content, "---\n") {
		return nil, &Warning{Path: path, Message: "no YAML front matter found (file must start with ---)"}
	}

	// Skip the opening ---\n (4 bytes) then find the closing ---.
	rest := content[4:]
	var yamlSrc, body string

	if idx := strings.Index(rest, "\n---\n"); idx != -1 {
		// Closing --- in the middle of the file.
		yamlSrc = rest[:idx]
		body = rest[idx+5:] // skip past \n---\n
	} else {
		// Check for closing --- at end of file (possibly with trailing newline).
		trimmed := strings.TrimRight(rest, "\r\n ")
		if strings.HasSuffix(trimmed, "\n---") {
			yamlSrc = trimmed[:len(trimmed)-4]
			body = ""
		} else {
			return nil, &Warning{Path: path, Message: "YAML front matter is not closed"}
		}
	}

	var fm frontMatter
	if err := yaml.Unmarshal([]byte(yamlSrc), &fm); err != nil {
		return nil, &Warning{Path: path, Message: fmt.Sprintf("invalid YAML front matter: %v", err)}
	}

	if fm.Assignee == "" {
		fm.Assignee = "UNKNOWN"
	}
	if fm.Status == "" {
		fm.Status = "UNKNOWN"
	}
	if fm.Priority == "" {
		fm.Priority = "UNKNOWN"
	}

	body = strings.TrimLeft(body, "\n")
	summary, description, commentHeading, comments := parseBody(body)

	id := strings.TrimSuffix(filename, ".md")

	return &Story{
		ID:             id,
		ColumnDir:      columnDir,
		Assignee:       fm.Assignee,
		Status:         fm.Status,
		Priority:       fm.Priority,
		Summary:        summary,
		Description:    description,
		CommentHeading: commentHeading,
		Comments:       comments,
	}, nil
}

// parseBody splits the markdown body into summary, description, comment heading,
// and comments.
// First H1 = summary. Text until next H1 = description. Second H1 text =
// commentHeading. Text after the second H1 = comments.
func parseBody(body string) (summary, description, commentHeading, comments string) {
	lines := strings.Split(body, "\n")

	firstH1 := -1
	secondH1 := -1

	for i, line := range lines {
		if strings.HasPrefix(line, "# ") {
			if firstH1 == -1 {
				firstH1 = i
			} else if secondH1 == -1 {
				secondH1 = i
				break
			}
		}
	}

	if firstH1 == -1 {
		// No H1 at all — everything is description.
		description = strings.TrimSpace(body)
		return
	}

	summary = strings.TrimPrefix(lines[firstH1], "# ")

	if secondH1 == -1 {
		// No second H1 — everything after first H1 is description.
		descLines := lines[firstH1+1:]
		description = strings.TrimSpace(strings.Join(descLines, "\n"))
		return
	}

	descLines := lines[firstH1+1 : secondH1]
	description = strings.TrimSpace(strings.Join(descLines, "\n"))

	commentHeading = strings.TrimPrefix(lines[secondH1], "# ")

	commentLines := lines[secondH1+1:]
	comments = strings.TrimSpace(strings.Join(commentLines, "\n"))
	return
}

// WriteStory serialises a story back to disk.
func WriteStory(dataDir string, story *Story) error {
	dir := filepath.Join(dataDir, story.ColumnDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create directory %s: %w", dir, err)
	}

	content := buildMarkdown(story)
	path := filepath.Join(dir, story.ID+".md")
	return os.WriteFile(path, []byte(content), 0o644)
}

// MoveStory moves a story's .md file from one column directory to another.
func MoveStory(dataDir, storyID, fromColumnDir, toColumnDir string) error {
	src := filepath.Join(dataDir, fromColumnDir, storyID+".md")
	dstDir := filepath.Join(dataDir, toColumnDir)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("cannot create directory: %w", err)
	}
	dst := filepath.Join(dstDir, storyID+".md")
	return os.Rename(src, dst)
}

// DeleteStory removes the .md file for a story.
func DeleteStory(dataDir, storyID, columnDir string) error {
	path := filepath.Join(dataDir, columnDir, storyID+".md")
	return os.Remove(path)
}

// CreateColumn creates a new ##.Name directory.
func CreateColumn(dataDir string, number int, name string) error {
	dirName := fmt.Sprintf("%02d.%s", number, name)
	path := filepath.Join(dataDir, dirName)
	return os.MkdirAll(path, 0o755)
}

// RenameColumn renames the folder for a column.
func RenameColumn(dataDir, oldDirName string, newNumber int, newName string) error {
	newDirName := fmt.Sprintf("%02d.%s", newNumber, newName)
	oldPath := filepath.Join(dataDir, oldDirName)
	newPath := filepath.Join(dataDir, newDirName)
	return os.Rename(oldPath, newPath)
}

// ReadStoryFromDisk reads and parses a single story file.
// Used for conflict detection during refresh.
func ReadStoryFromDisk(dataDir, columnDir, storyID string) (*Story, *Warning) {
	filename := storyID + ".md"
	path := filepath.Join(dataDir, columnDir, filename)
	return parseStory(path, columnDir, filename)
}

// StoryExists reports whether the file for a story exists on disk.
func StoryExists(dataDir, columnDir, storyID string) bool {
	path := filepath.Join(dataDir, columnDir, storyID+".md")
	_, err := os.Stat(path)
	return err == nil
}

func buildMarkdown(s *Story) string {
	var sb strings.Builder

	// Marshal front matter via the yaml library so special characters in
	// free-form values are quoted correctly.
	fm := frontMatter{
		Assignee: s.Assignee,
		Status:   s.Status,
		Priority: s.Priority,
	}
	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		// Fallback: write raw (should never happen with simple string values).
		fmBytes = []byte(fmt.Sprintf("assignee: %s\nstatus: %s\npriority: %s\n",
			s.Assignee, s.Status, s.Priority))
	}
	sb.WriteString("---\n")
	sb.Write(fmBytes)
	sb.WriteString("---\n\n")
	sb.WriteString(fmt.Sprintf("# %s\n", s.Summary))
	if s.Description != "" {
		sb.WriteString("\n")
		sb.WriteString(s.Description)
		sb.WriteString("\n")
	}

	// Preserve whatever the user called their comment section heading,
	// defaulting to "Commentary" for new stories.
	heading := s.CommentHeading
	if heading == "" {
		heading = "Commentary"
	}
	sb.WriteString(fmt.Sprintf("\n# %s\n", heading))
	if s.Comments != "" {
		sb.WriteString("\n")
		sb.WriteString(s.Comments)
		sb.WriteString("\n")
	}
	return sb.String()
}
