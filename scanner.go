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
	Priority string `yaml:"priority"`
}

// Scan reads dataDir and returns the full board state.
func Scan(dataDir string) (*Board, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read data directory: %w", err)
	}

	var warnings []Warning
	seenNumbers := map[int]string{}
	var columns []*Column

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m := columnDirRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
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

	sort.Slice(columns, func(i, j int) bool {
		return columns[i].Number < columns[j].Number
	})

	return &Board{
		Columns:  columns,
		Warnings: warnings,
	}, nil
}

// parseStory parses a single .md file.
func parseStory(path, columnDir, filename string) (*Story, *Warning) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &Warning{Path: path, Message: fmt.Sprintf("cannot read file: %v", err)}
	}

	content := string(data)

	if !strings.HasPrefix(content, "---\n") {
		return nil, &Warning{Path: path, Message: "no YAML front matter found (file must start with ---)"}
	}

	rest := content[4:]
	var yamlSrc, body string

	if idx := strings.Index(rest, "\n---\n"); idx != -1 {
		yamlSrc = rest[:idx]
		body = rest[idx+5:]
	} else {
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
		Priority:       fm.Priority,
		Summary:        summary,
		Description:    description,
		CommentHeading: commentHeading,
		Comments:       comments,
	}, nil
}

// parseBody splits the markdown body into summary, description, comment heading,
// and comments.
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
		description = strings.TrimSpace(body)
		return
	}

	summary = strings.TrimPrefix(lines[firstH1], "# ")

	if secondH1 == -1 {
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

	fm := frontMatter{
		Assignee: s.Assignee,
		Priority: s.Priority,
	}
	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		fmBytes = []byte(fmt.Sprintf("assignee: %s\npriority: %s\n", s.Assignee, s.Priority))
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
