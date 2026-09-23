package office

import (
	"fmt"
	"strings"
)

// buildPptx builds a minimal but valid .pptx.
//
// It's the most demanding format of the three: a slide does not open on its own — it needs a
// layout, which needs a global model (master), which needs a theme. A package missing any one of
// them gets rejected by PowerPoint with "the file is corrupted," without saying what's missing.
// Hence all of them being here, even the ones that aren't visible.
//
// The design is deliberately plain: title and bullets, theme fonts. Generating pretty slides
// is a different problem, and solving it with hand-written XML would be the wrong trade-off.
func buildPptx(p Plan) ([]byte, error) {
	slides := p.Slides
	if len(slides) == 0 {
		// A presentation with no slides is not a file that opens. If there's a title, make the
		// cover slide with it; otherwise, an empty slide — better than an error over a thin plan.
		slides = []Slide{{Title: p.Title}}
	}

	parts := []part{}
	var slideRefs, presRels, overrides strings.Builder

	for i, s := range slides {
		n := i + 1
		path := fmt.Sprintf("ppt/slides/slide%d.xml", n)
		rid := fmt.Sprintf("rId%d", n+1) // rId1 is reserved for the master
		fmt.Fprintf(&slideRefs, `<p:sldId id="%d" r:id="%s"/>`, 255+n, rid)
		fmt.Fprintf(&presRels, `<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide%d.xml"/>`, rid, n)
		fmt.Fprintf(&overrides, `<Override PartName="/%s" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`, path)
		parts = append(parts,
			part{path, slideXML(s)},
			part{fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", n), xmlHeader +
				`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
				`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>` +
				`</Relationships>`})
	}

	head := []part{
		{"[Content_Types].xml", xmlHeader +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/ppt/presentation.xml" ContentType="` + MIME(KindPresentation) + `.main+xml"/>` +
			`<Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/>` +
			`<Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/>` +
			`<Override PartName="/ppt/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/>` +
			overrides.String() +
			`</Types>`},
		{"_rels/.rels", xmlHeader +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>` +
			`</Relationships>`},
		{"ppt/presentation.xml", xmlHeader +
			`<p:presentation xmlns:a="` + nsDrawing + `" xmlns:r="` + nsRel + `" xmlns:p="` + nsPres + `">` +
			`<p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst>` +
			`<p:sldIdLst>` + slideRefs.String() + `</p:sldIdLst>` +
			// 16:9 in EMU (English Metric Units). It's the format everyone expects today.
			`<p:sldSz cx="12192000" cy="6858000"/><p:notesSz cx="6858000" cy="9144000"/>` +
			`</p:presentation>`},
		{"ppt/_rels/presentation.xml.rels", xmlHeader +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="slideMasters/slideMaster1.xml"/>` +
			presRels.String() +
			fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="theme/theme1.xml"/>`, len(slides)+2) +
			`</Relationships>`},
		{"ppt/slideMasters/slideMaster1.xml", slideMasterXML},
		{"ppt/slideMasters/_rels/slideMaster1.xml.rels", xmlHeader +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="../theme/theme1.xml"/>` +
			`</Relationships>`},
		{"ppt/slideLayouts/slideLayout1.xml", slideLayoutXML},
		{"ppt/slideLayouts/_rels/slideLayout1.xml.rels", xmlHeader +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/slideMaster1.xml"/>` +
			`</Relationships>`},
		{"ppt/theme/theme1.xml", themeXML},
	}
	return zipParts(append(head, parts...))
}

const (
	nsDrawing = "http://schemas.openxmlformats.org/drawingml/2006/main"
	nsRel     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsPres    = "http://schemas.openxmlformats.org/presentationml/2006/main"
)

// Design palette. Explicit srgbClr (not schemeClr) so the result is the same in any
// reader, without depending on the theme's color map.
const (
	accentHex  = "116466" // Beskos green — title bands and cover
	accent2Hex = "0C4A4C" // darker tone — thin accent under the band
	lightHex   = "F4F1EC" // cream — decorative rule and subtitle on the cover
	inkHex     = "1C1B1A" // near-black — bullet text
)

// Whole slide: 16:9 in EMU. Constants to position the boxes without loose numbers in the middle.
const (
	slideW   = 12192000
	slideH   = 6858000
	bandH    = 1500000 // title band height on content slides
	marginX  = 838200
	contentW = slideW - 2*marginX
)

// slideXML picks the design by content: a slide WITHOUT bullets is a COVER (colored background,
// large centered title); a slide WITH bullets is a CONTENT slide (colored title band + styled
// bullets). This way the opening slide, which the model usually makes with just a title, gets
// a cover look without needing a new field in the plan.
func slideXML(s Slide) string {
	if !hasBullets(s) {
		return coverSlide(s)
	}
	return contentSlide(s)
}

func hasBullets(s Slide) bool {
	for _, b := range s.Bullets {
		if strings.TrimSpace(b) != "" {
			return true
		}
	}
	return false
}

// coverSlide: solid accent-colored background, decorative rule, and a large white title.
func coverSlide(s Slide) string {
	shapes := rectXML(2, "BG", 0, 0, slideW, slideH, accentHex) +
		rectXML(3, "Rule", marginX, 3630000, 2600000, 50000, lightHex) +
		textBoxXML(4, "Title", marginX, 2500000, contentW, 1300000, "b",
			paraXML(s.Title, 5400, "FFFFFF", true, "l"))
	return slideWrap(shapes)
}

// contentSlide: colored title band at the top (white title) and bullets below in dark text,
// each with a marker in the accent color.
func contentSlide(s Slide) string {
	var bullets strings.Builder
	for _, b := range s.Bullets {
		if strings.TrimSpace(b) == "" {
			continue
		}
		bullets.WriteString(bulletXML(b))
	}
	if bullets.Len() == 0 {
		bullets.WriteString(`<a:p/>`)
	}
	shapes := rectXML(2, "TitleBand", 0, 0, slideW, bandH, accentHex) +
		rectXML(3, "Accent", 0, bandH, slideW, 60000, accent2Hex) +
		textBoxXML(4, "Title", marginX, 0, contentW, bandH, "ctr",
			paraXML(s.Title, 3200, "FFFFFF", true, "l")) +
		textBoxXML(5, "Content", marginX, bandH+400000, contentW, slideH-bandH-800000, "t", bullets.String())
	return slideWrap(shapes)
}

// slideWrap wraps the shapes in a slide's XML.
func slideWrap(shapes string) string {
	return xmlHeader +
		`<p:sld xmlns:a="` + nsDrawing + `" xmlns:r="` + nsRel + `" xmlns:p="` + nsPres + `">` +
		`<p:cSld><p:spTree>` +
		`<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
		`<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/>` +
		`<a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>` +
		shapes +
		`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`
}

// rectXML is a solid-color-filled rectangle (bands, background, rules). No text.
func rectXML(id int, name string, x, y, cx, cy int, fill string) string {
	return fmt.Sprintf(
		`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>`+
			`<p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>`+
			`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom>`+
			`<a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:ln><a:noFill/></a:ln></p:spPr>`+
			`<p:txBody><a:bodyPr/><a:lstStyle/><a:p/></p:txBody></p:sp>`,
		id, name, x, y, cx, cy, fill)
}

// textBoxXML is a positioned text box; anchor is the vertical alignment (t|ctr|b).
func textBoxXML(id int, name string, x, y, cx, cy int, anchor, body string) string {
	return fmt.Sprintf(
		`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>`+
			`<p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>`+
			`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>`+
			`<p:txBody><a:bodyPr wrap="square" anchor="%s"><a:normAutofit/></a:bodyPr><a:lstStyle/>%s</p:txBody></p:sp>`,
		id, name, x, y, cx, cy, anchor, body)
}

// paraXML is a paragraph with one formatted text run (size, color, bold, alignment).
func paraXML(text string, sz int, color string, bold bool, align string) string {
	b := "0"
	if bold {
		b = "1"
	}
	return fmt.Sprintf(
		`<a:p><a:pPr algn="%s"/><a:r><a:rPr lang="en-US" sz="%d" b="%s"><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:rPr><a:t>%s</a:t></a:r></a:p>`,
		align, sz, b, color, esc(text))
}

// bulletXML is a bullet point with an accent-colored marker and dark text, with a hanging indent.
func bulletXML(text string) string {
	return `<a:p><a:pPr marL="285750" indent="-285750"><a:spcBef><a:spcPts val="600"/></a:spcBef>` +
		`<a:buClr><a:srgbClr val="` + accentHex + `"/></a:buClr><a:buFont typeface="Arial"/><a:buChar char="&#8226;"/></a:pPr>` +
		`<a:r><a:rPr lang="en-US" sz="2000"><a:solidFill><a:srgbClr val="` + inkHex + `"/></a:solidFill></a:rPr>` +
		`<a:t>` + esc(text) + `</a:t></a:r></a:p>`
}

// slideMasterXML is the global model. It isn't visible, but without it the package is rejected.
const slideMasterXML = xmlHeader +
	`<p:sldMaster xmlns:a="` + nsDrawing + `" xmlns:r="` + nsRel + `" xmlns:p="` + nsPres + `">` +
	`<p:cSld><p:spTree>` +
	`<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
	`<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/>` +
	`<a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>` +
	`</p:spTree></p:cSld>` +
	// The color map links the roles (background, text, accent) to the theme's colors. It's mandatory.
	`<p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" ` +
	`accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>` +
	`<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst>` +
	`</p:sldMaster>`

// slideLayoutXML is the layout the slides declare they follow.
const slideLayoutXML = xmlHeader +
	`<p:sldLayout xmlns:a="` + nsDrawing + `" xmlns:r="` + nsRel + `" xmlns:p="` + nsPres + `" type="blank">` +
	`<p:cSld name="Blank"><p:spTree>` +
	`<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
	`<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/>` +
	`<a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>` +
	`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`

// themeXML is the minimal theme. The schema requires the six accent colors, the two link
// colors, the two light and two dark ones, plus the font and format schemes — none is optional.
const themeXML = xmlHeader +
	`<a:theme xmlns:a="` + nsDrawing + `" name="Beskos">` +
	`<a:themeElements>` +
	`<a:clrScheme name="Beskos">` +
	`<a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1>` +
	`<a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1>` +
	`<a:dk2><a:srgbClr val="1C1B1A"/></a:dk2>` +
	`<a:lt2><a:srgbClr val="F4F1EC"/></a:lt2>` +
	`<a:accent1><a:srgbClr val="116466"/></a:accent1>` +
	`<a:accent2><a:srgbClr val="3B6FB0"/></a:accent2>` +
	`<a:accent3><a:srgbClr val="2E7D5B"/></a:accent3>` +
	`<a:accent4><a:srgbClr val="C0692F"/></a:accent4>` +
	`<a:accent5><a:srgbClr val="8257C7"/></a:accent5>` +
	`<a:accent6><a:srgbClr val="B44D3A"/></a:accent6>` +
	`<a:hlink><a:srgbClr val="3B6FB0"/></a:hlink>` +
	`<a:folHlink><a:srgbClr val="8257C7"/></a:folHlink>` +
	`</a:clrScheme>` +
	`<a:fontScheme name="Beskos">` +
	`<a:majorFont><a:latin typeface="Calibri Light"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont>` +
	`<a:minorFont><a:latin typeface="Calibri"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont>` +
	`</a:fontScheme>` +
	`<a:fmtScheme name="Beskos">` +
	`<a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill>` +
	`<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>` +
	`<a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:fillStyleLst>` +
	`<a:lnStyleLst><a:ln><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln>` +
	`<a:ln><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln>` +
	`<a:ln><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln></a:lnStyleLst>` +
	`<a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle>` +
	`<a:effectStyle><a:effectLst/></a:effectStyle>` +
	`<a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst>` +
	`<a:bgFillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill>` +
	`<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>` +
	`<a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:bgFillStyleLst>` +
	`</a:fmtScheme>` +
	`</a:themeElements></a:theme>`
