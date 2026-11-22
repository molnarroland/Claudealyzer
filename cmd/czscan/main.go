// czscan runs a full index scan and prints totals (development aid).
package main

import (
	"context"
	"fmt"
	"time"

	"claudealyzer/internal/core"
)

func main() {
	c := core.New(nil)
	t0 := time.Now()
	n := len(c.Scan(context.Background()))
	fmt.Printf("parsed %d sessions in %v\n", n, time.Since(t0))
	for _, s := range c.Sources() {
		fmt.Printf("%-18s %-7s %4d sessions $%9.2f  %s | %s\n", s.Name, s.Agent, s.Sessions, s.CostUSD, s.Version, s.Detection)
	}
	ps := c.Projects("all", "all", core.Filter{})
	for i, p := range ps {
		if i > 8 {
			break
		}
		fmt.Printf("  %-30s %v %3d $%.2f\n", p.Name, p.Agents, p.SessionCount, p.CostUSD)
	}
	rows := c.Sessions("", "all", "all", "cost", core.Filter{})
	top := rows[0]
	fmt.Printf("top: %s %q $%.2f high=%d med=%d\n", top.SourceID, top.Title, top.Usage.CostUSD, top.FindingsHigh, top.FindingsMed)
	d, err := c.Session(top.SourceID, top.ID)
	if err != nil { panic(err) }
	for _, f := range d.Findings { fmt.Printf("   [%s] %s — %s ($%.2f)\n", f.Severity, f.Title, f.Text, f.ImpactUSD) }
	a, _ := c.Analysis(top.SourceID, top.ID, "")
	fmt.Printf("analysis: turns=%d time=%v cmds=%d files=%d\n", len(a.Turns), a.Time, len(a.Commands), len(a.Files))
	cr, _ := c.CostReport(top.SourceID, top.ID, 0.7)
	for _, s := range cr.Scenarios { fmt.Printf("   %-45s $%.2f\n", s.Name, s.Cost) }
	o := c.Overview("", "all", "all", 30, core.Filter{})
	fmt.Printf("overview 30d: $%.2f sessions=%d days=%d\n", o.TotalUSD, o.Sessions, len(o.Days))
	t0 = time.Now(); c2 := core.New(nil); c2.Scan(context.Background()); fmt.Println("cached rescan", time.Since(t0))
}
