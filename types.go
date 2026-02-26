package main

// Column represents a ##.Name folder on disk.
type Column struct {
	Number int    // parsed from the ## prefix
	Name   string // the part after the dot
	DirName string // full folder name, e.g. "01.Backlog"
	Stories []*Story
}

// Story represents a single .md file.
type Story struct {
	ID              string // filename minus .md extension
	ColumnDir       string // the folder DirName it lives in
	Assignee        string
	Status          string
	Priority        string
	Summary         string
	Description     string
	CommentHeading  string // the text of the second H1 (e.g. "Commentary")
	Comments        string
}

// Warning represents a non-fatal issue found during scanning.
type Warning struct {
	Path    string
	Message string
}

// Board is the full in-memory state.
type Board struct {
	Columns  []*Column
	Warnings []Warning
}
