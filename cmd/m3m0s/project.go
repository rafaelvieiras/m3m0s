package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/rafaelvieiras/m3m0s/internal/project"
)

// runProject is `m3m0s project`: it manages ~/.m3m0s/projects.json, the
// directory -> project mapping the hooks and the MCP server use to scope a
// session (D34). Nothing is written into the repositories.
func runProject(args []string) error {
	home, err := expandHome("~")
	if err != nil {
		return err
	}
	if len(args) == 0 {
		args = []string{"show"}
	}
	m, err := project.Load(home)
	if err != nil {
		return err
	}

	switch args[0] {
	case "show":
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if p := project.Resolve(os.Getenv, home, cwd); p != "" {
			fmt.Printf("%s (for %s)\n", p, cwd)
		} else {
			fmt.Printf("none: %s uses the global memory only\n", cwd)
		}
		return nil
	case "list":
		if len(m) == 0 {
			fmt.Println("no directories mapped (", project.Path(home), ")")
			return nil
		}
		for _, d := range m.Dirs() {
			fmt.Printf("%-24s %s\n", m[d], d)
		}
		return nil
	case "set":
		if len(args) < 2 || len(args) > 3 {
			return errors.New("usage: m3m0s project set <slug> [dir]  (dir defaults to the current directory)")
		}
		dir := "."
		if len(args) == 3 {
			dir = args[2]
		}
		if err := m.Set(dir, args[1]); err != nil {
			return err
		}
		if err := m.Save(home); err != nil {
			return err
		}
		fmt.Printf("mapped %s -> %s\n", dir, args[1])
		return nil
	case "unset":
		if len(args) > 2 {
			return errors.New("usage: m3m0s project unset [dir]")
		}
		dir := "."
		if len(args) == 2 {
			dir = args[1]
		}
		removed, err := m.Unset(dir)
		if err != nil {
			return err
		}
		if !removed {
			return fmt.Errorf("%s is not mapped", dir)
		}
		if err := m.Save(home); err != nil {
			return err
		}
		fmt.Println("unmapped", dir)
		return nil
	default:
		return errors.New("usage: m3m0s project [show|list|set <slug> [dir]|unset [dir]]")
	}
}
