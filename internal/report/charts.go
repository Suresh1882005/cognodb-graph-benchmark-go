package report

import (
	"fmt"
	"os"
	"strings"

	"github.com/suresh/cognodb-graph-benchmark/internal/harness"
)

// A tiny, dependency-free SVG chart kit. No charting library — this repo's
// only external dependencies are the three official database drivers it
// benchmarks, so charts are built as plain SVG (which is just XML text).

var palette = []string{"#2563eb", "#dc2626", "#16a34a", "#9333ea", "#ea580c"}

const (
	chartW, chartH = 760.0, 420.0
	marginL        = 60.0
	marginR        = 20.0
	marginT        = 40.0
	marginB        = 90.0
)

func plotArea() (x0, y0, x1, y1 float64) {
	return marginL, marginT, chartW - marginR, chartH - marginB
}

func svgHeader(title string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg viewBox="0 0 %.0f %.0f" xmlns="http://www.w3.org/2000/svg" font-family="Helvetica, Arial, sans-serif">`, chartW, chartH)
	sb.WriteString(`<rect width="100%" height="100%" fill="white"/>`)
	fmt.Fprintf(&sb, `<text x="%.0f" y="24" font-size="18" font-weight="bold" fill="#111">%s</text>`, marginL, escapeXML(title))
	return sb.String()
}

func escapeXML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;")
	return r.Replace(s)
}

// groupedBarChart draws len(series) bars per category, one color per series.
func groupedBarChart(title string, categories []string, seriesNames []string, seriesValues [][]float64, yLabel string) string {
	x0, y0, x1, y1 := plotArea()
	plotW := x1 - x0
	plotH := y1 - y0

	maxV := 0.0
	for _, vals := range seriesValues {
		for _, v := range vals {
			if v > maxV {
				maxV = v
			}
		}
	}
	if maxV == 0 {
		maxV = 1
	}

	var sb strings.Builder
	sb.WriteString(svgHeader(title))

	// axes
	fmt.Fprintf(&sb, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#333" stroke-width="1"/>`, x0, y0, x0, y1)
	fmt.Fprintf(&sb, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#333" stroke-width="1"/>`, x0, y1, x1, y1)
	fmt.Fprintf(&sb, `<text x="%.1f" y="%.1f" font-size="11" fill="#333" transform="rotate(-90 %.1f %.1f)">%s</text>`,
		18.0, (y0+y1)/2, 18.0, (y0+y1)/2, escapeXML(yLabel))

	// y gridlines + labels (5 ticks)
	for i := 0; i <= 4; i++ {
		frac := float64(i) / 4
		y := y1 - frac*plotH
		val := frac * maxV
		fmt.Fprintf(&sb, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#eee" stroke-width="1"/>`, x0, y, x1, y)
		fmt.Fprintf(&sb, `<text x="%.1f" y="%.1f" font-size="10" fill="#666" text-anchor="end">%.1f</text>`, x0-6, y+3, val)
	}

	nCats := len(categories)
	nSeries := len(seriesNames)
	if nCats == 0 || nSeries == 0 {
		sb.WriteString(`</svg>`)
		return sb.String()
	}
	catWidth := plotW / float64(nCats)
	barGroupW := catWidth * 0.7
	barW := barGroupW / float64(nSeries)

	for ci, cat := range categories {
		catX0 := x0 + float64(ci)*catWidth + (catWidth-barGroupW)/2
		for si := range seriesNames {
			v := 0.0
			if ci < len(seriesValues[si]) {
				v = seriesValues[si][ci]
			}
			barH := (v / maxV) * plotH
			bx := catX0 + float64(si)*barW
			by := y1 - barH
			color := palette[si%len(palette)]
			fmt.Fprintf(&sb, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`, bx, by, barW*0.9, barH, color)
		}
		labelX := x0 + float64(ci)*catWidth + catWidth/2
		fmt.Fprintf(&sb, `<text x="%.1f" y="%.1f" font-size="10" fill="#333" text-anchor="middle" transform="rotate(-20 %.1f %.1f)">%s</text>`,
			labelX, y1+16, labelX, y1+16, escapeXML(cat))
	}

	// legend
	legendX := x0
	legendY := y1 + 60
	for si, name := range seriesNames {
		lx := legendX + float64(si)*160
		fmt.Fprintf(&sb, `<rect x="%.1f" y="%.1f" width="10" height="10" fill="%s"/>`, lx, legendY, palette[si%len(palette)])
		fmt.Fprintf(&sb, `<text x="%.1f" y="%.1f" font-size="11" fill="#333">%s</text>`, lx+14, legendY+9, escapeXML(name))
	}

	sb.WriteString(`</svg>`)
	return sb.String()
}

// lineChart draws one line per series across shared numeric x categories.
func lineChart(title string, xValues []float64, seriesNames []string, seriesValues [][]float64, xLabel, yLabel string) string {
	x0, y0, x1, y1 := plotArea()
	plotW := x1 - x0
	plotH := y1 - y0

	maxY := 0.0
	for _, vals := range seriesValues {
		for _, v := range vals {
			if v > maxY {
				maxY = v
			}
		}
	}
	if maxY == 0 {
		maxY = 1
	}
	maxX, minX := xValues[0], xValues[0]
	for _, x := range xValues {
		if x > maxX {
			maxX = x
		}
		if x < minX {
			minX = x
		}
	}
	xRange := maxX - minX
	if xRange == 0 {
		xRange = 1
	}

	var sb strings.Builder
	sb.WriteString(svgHeader(title))
	fmt.Fprintf(&sb, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#333" stroke-width="1"/>`, x0, y0, x0, y1)
	fmt.Fprintf(&sb, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#333" stroke-width="1"/>`, x0, y1, x1, y1)
	fmt.Fprintf(&sb, `<text x="%.1f" y="%.1f" font-size="11" fill="#333">%s</text>`, (x0+x1)/2-20, y1+40, escapeXML(xLabel))
	fmt.Fprintf(&sb, `<text x="18" y="%.1f" font-size="11" fill="#333" transform="rotate(-90 18 %.1f)">%s</text>`, (y0+y1)/2, (y0+y1)/2, escapeXML(yLabel))

	for i := 0; i <= 4; i++ {
		frac := float64(i) / 4
		y := y1 - frac*plotH
		val := frac * maxY
		fmt.Fprintf(&sb, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#eee" stroke-width="1"/>`, x0, y, x1, y)
		fmt.Fprintf(&sb, `<text x="%.1f" y="%.1f" font-size="10" fill="#666" text-anchor="end">%.0f</text>`, x0-6, y+3, val)
	}
	for _, xv := range xValues {
		px := x0 + ((xv-minX)/xRange)*plotW
		fmt.Fprintf(&sb, `<text x="%.1f" y="%.1f" font-size="10" fill="#333" text-anchor="middle">%.0f</text>`, px, y1+16, xv)
	}

	for si, name := range seriesNames {
		color := palette[si%len(palette)]
		var points []string
		for i, xv := range xValues {
			if i >= len(seriesValues[si]) {
				continue
			}
			px := x0 + ((xv-minX)/xRange)*plotW
			py := y1 - (seriesValues[si][i]/maxY)*plotH
			points = append(points, fmt.Sprintf("%.1f,%.1f", px, py))
			fmt.Fprintf(&sb, `<circle cx="%.1f" cy="%.1f" r="3" fill="%s"/>`, px, py, color)
		}
		fmt.Fprintf(&sb, `<polyline points="%s" fill="none" stroke="%s" stroke-width="2"/>`, strings.Join(points, " "), color)

		legendY := y1 + 60 + float64(si)*16
		fmt.Fprintf(&sb, `<rect x="%.1f" y="%.1f" width="10" height="10" fill="%s"/>`, x0, legendY, color)
		fmt.Fprintf(&sb, `<text x="%.1f" y="%.1f" font-size="11" fill="#333">%s</text>`, x0+14, legendY+9, escapeXML(name))
	}

	sb.WriteString(`</svg>`)
	return sb.String()
}

func chartIngestThroughput(keys []string, results map[string]harness.Result, outPath string) error {
	var cats []string
	var nodesSec, edgesSec []float64
	for _, k := range keys {
		r := results[k]
		if r.Ingest == nil {
			continue
		}
		cats = append(cats, r.DisplayName)
		nodesSec = append(nodesSec, r.Ingest.NodesPerSec)
		edgesSec = append(edgesSec, r.Ingest.EdgesPerSec)
	}
	if len(cats) == 0 {
		return os.WriteFile(outPath, []byte(emptyChart("Ingest throughput by platform (no data yet)")), 0o644)
	}
	svg := groupedBarChart("Ingest throughput by platform", cats,
		[]string{"nodes/sec", "edges/sec"}, [][]float64{nodesSec, edgesSec}, "throughput (items/sec)")
	return os.WriteFile(outPath, []byte(svg), 0o644)
}

func chartTraversalP95(keys []string, results map[string]harness.Result, outPath string) error {
	hopLabels := []string{"1-hop", "2-hop", "3-hop"}
	hopKeys := []string{"1_hop", "2_hop", "3_hop"}

	var seriesNames []string
	var seriesValues [][]float64
	for _, k := range keys {
		r := results[k]
		if len(r.Traversals) == 0 {
			continue
		}
		seriesNames = append(seriesNames, r.DisplayName)
		var vals []float64
		for _, hk := range hopKeys {
			if t, ok := r.Traversals[hk]; ok && t.Summary.P95Ms != nil {
				vals = append(vals, *t.Summary.P95Ms)
			} else {
				vals = append(vals, 0)
			}
		}
		seriesValues = append(seriesValues, vals)
	}
	if len(seriesNames) == 0 {
		return os.WriteFile(outPath, []byte(emptyChart("Traversal p95 latency by hop depth (no data yet)")), 0o644)
	}
	svg := groupedBarChart("Traversal p95 latency by hop depth", hopLabels, seriesNames, seriesValues, "p95 latency (ms)")
	return os.WriteFile(outPath, []byte(svg), 0o644)
}

func chartMixedThroughput(keys []string, results map[string]harness.Result, outPath string) error {
	// collect the union of concurrency levels actually present, sorted
	concSet := map[int]bool{}
	for _, k := range keys {
		for ck := range results[k].MixedWorkload {
			var c int
			fmt.Sscanf(ck, "%d", &c)
			concSet[c] = true
		}
	}
	var xValues []float64
	for c := range concSet {
		xValues = append(xValues, float64(c))
	}
	sortFloats(xValues)

	if len(xValues) == 0 {
		return os.WriteFile(outPath, []byte(emptyChart("Mixed read/write throughput vs. concurrency (no data yet)")), 0o644)
	}

	var seriesNames []string
	var seriesValues [][]float64
	for _, k := range keys {
		r := results[k]
		if len(r.MixedWorkload) == 0 {
			continue
		}
		seriesNames = append(seriesNames, r.DisplayName)
		var vals []float64
		for _, xv := range xValues {
			key := fmt.Sprintf("%d", int(xv))
			if m, ok := r.MixedWorkload[key]; ok {
				vals = append(vals, m.ThroughputQPS)
			} else {
				vals = append(vals, 0)
			}
		}
		seriesValues = append(seriesValues, vals)
	}
	svg := lineChart("Mixed read/write throughput vs. concurrency", xValues, seriesNames, seriesValues, "client concurrency", "throughput (qps)")
	return os.WriteFile(outPath, []byte(svg), 0o644)
}

func sortFloats(v []float64) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j-1] > v[j]; j-- {
			v[j-1], v[j] = v[j], v[j-1]
		}
	}
}

func emptyChart(title string) string {
	var sb strings.Builder
	sb.WriteString(svgHeader(title))
	fmt.Fprintf(&sb, `<text x="%.0f" y="%.0f" font-size="13" fill="#999" text-anchor="middle">no data yet — run the benchmark for at least one platform</text>`, chartW/2, chartH/2)
	sb.WriteString(`</svg>`)
	return sb.String()
}
