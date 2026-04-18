package markdown

import "strings"

func codeBlockToDiagram(cb *Node) *Node {
	if cb == nil || cb.Type != NodeCodeBlock {
		return cb
	}
	syntax, kind, ok := diagramFenceInfo(cb.Attrs["language"], cb.Literal)
	if !ok {
		return cb
	}
	return &Node{
		Type:    NodeDiagram,
		Literal: cb.Literal,
		Attrs: map[string]string{
			"language": cb.Attrs["language"],
			"syntax":   syntax,
			"kind":     kind,
		},
	}
}

func diagramFenceInfo(language, source string) (syntax, kind string, ok bool) {
	lang := normalizedFenceLanguage(language)
	switch lang {
	case "mermaid", "mmd":
		return "mermaid", inferMermaidDiagramKind(source), true
	case "flow", "flowchart", "graph":
		return "mermaid", "flowchart", true
	case "erd", "er", "erdiagram":
		return "mermaid", "erd", true
	case "sequence", "sequencediagram":
		return "mermaid", "sequence", true
	case "class", "classdiagram":
		return "mermaid", "class", true
	case "state", "statediagram", "statediagram-v2":
		return "mermaid", "state", true
	case "gantt", "journey", "pie", "mindmap", "timeline":
		return "mermaid", lang, true
	case "gitgraph":
		return "mermaid", "gitgraph", true
	}
	return "", "", false
}

func normalizedFenceLanguage(language string) string {
	fields := strings.Fields(strings.TrimSpace(language))
	if len(fields) == 0 {
		return ""
	}
	lang := strings.ToLower(fields[0])
	lang = strings.TrimPrefix(lang, "{.")
	lang = strings.TrimPrefix(lang, ".")
	lang = strings.TrimSuffix(lang, "}")
	return strings.ReplaceAll(lang, "_", "-")
}

func inferMermaidDiagramKind(source string) string {
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		directive := strings.ToLower(strings.Trim(fields[0], ":{"))
		directive = strings.ReplaceAll(directive, "_", "-")
		switch directive {
		case "flowchart", "graph":
			return "flowchart"
		case "erdiagram":
			return "erd"
		case "sequencediagram":
			return "sequence"
		case "classdiagram":
			return "class"
		case "statediagram", "statediagram-v2":
			return "state"
		case "gantt", "journey", "pie", "mindmap", "timeline", "gitgraph":
			return directive
		case "requirementdiagram":
			return "requirement"
		case "c4context":
			return "c4"
		case "quadrantchart":
			return "quadrant"
		case "xychart-beta":
			return "xychart"
		case "block-beta":
			return "block"
		case "sankey-beta":
			return "sankey"
		case "packet-beta":
			return "packet"
		case "architecture-beta":
			return "architecture"
		default:
			return "mermaid"
		}
	}
	return "mermaid"
}
