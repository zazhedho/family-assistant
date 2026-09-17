package main

import (
	"flag"
	"fmt"
	"github.com/zazhedho/family-assistant/pkg/moduleseed"
	"log"
	"os"
	"strings"
)

func main() {
	var (
		name        string
		displayName string
		path        string
		icon        string
		parentName  string
		resource    string
		actions     string
		grantRoles  string
		orderIndex  int
	)

	flag.StringVar(&name, "name", "", "menu and module name, for example: projects")
	flag.StringVar(&displayName, "display-name", "", "menu display name, for example: Projects")
	flag.StringVar(&path, "path", "", "frontend path, for example: /projects")
	flag.StringVar(&icon, "icon", "bi-folder", "bootstrap icon name")
	flag.StringVar(&parentName, "parent-name", "", "optional parent menu name")
	flag.StringVar(&resource, "resource", "", "optional permission resource, defaults to name")
	flag.StringVar(&actions, "actions", "list,view,create,update,delete", "comma-separated actions")
	flag.StringVar(&grantRoles, "grant-roles", "admin,superadmin", "comma-separated roles for default grants, empty to skip")
	flag.IntVar(&orderIndex, "order-index", 900, "menu order index")
	flag.Parse()

	sql, err := moduleseed.RenderSQL(moduleseed.Definition{
		Name:        name,
		DisplayName: displayName,
		Path:        path,
		Icon:        icon,
		OrderIndex:  orderIndex,
		ParentName:  parentName,
		Resource:    resource,
		Actions:     splitCSV(actions),
		GrantRoles:  splitCSV(grantRoles),
	})
	if err != nil {
		log.Fatal(err)
	}

	if _, err := fmt.Fprintln(os.Stdout, sql); err != nil {
		log.Fatal(err)
	}
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}
