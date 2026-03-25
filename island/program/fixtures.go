package program

// TabsProgram returns a reference Program for a tab-switching component.
//
// Three tabs (About, Features, Contact) with conditional rendering via OpCond.
// One signal "activeTab" (TypeInt, init 0), three handlers that each set
// activeTab to 0, 1, or 2. The tab content is a nested conditional expression.
//
// CSS class toggling: each tab button has an AttrExpr that evaluates to
// "tab-btn active" when that tab is selected and "tab-btn" otherwise,
// demonstrating dynamic class switching — a core SPA pattern.
func TabsProgram() *Program {
	// Expressions:
	//   0: LitInt "0"                          — initial value for activeTab
	//   1: SignalGet "activeTab"                — read activeTab (for display expr)
	//   2: LitInt "0"                          — literal 0 (comparison)
	//   3: OpEq [1, 2]                         — activeTab == 0
	//   4: LitString "About: GoSX..."          — about text
	//   5: SignalGet "activeTab"                — read activeTab (for inner cond)
	//   6: LitInt "1"                          — literal 1 (comparison)
	//   7: OpEq [5, 6]                         — activeTab == 1
	//   8: LitString "Features: Server-first..." — features text
	//   9: LitString "Contact: github.com..."  — contact text
	//  10: OpCond [7, 8, 9]                    — inner cond: activeTab==1 ? features : contact
	//  11: OpCond [3, 4, 10]                   — outer cond: activeTab==0 ? about : inner
	//  12: SignalSet "activeTab" <- [13]        — set activeTab = 0
	//  13: LitInt "0"                          — literal 0 (for set)
	//  14: SignalSet "activeTab" <- [15]        — set activeTab = 1
	//  15: LitInt "1"                          — literal 1 (for set)
	//  16: SignalSet "activeTab" <- [17]        — set activeTab = 2
	//  17: LitInt "2"                          — literal 2 (for set)
	//
	// CSS class toggling expressions (dynamic class per button):
	//  18: SignalGet "activeTab"                — for button 0 class
	//  19: LitInt "0"                          — comparison target
	//  20: OpEq [18, 19]                       — activeTab == 0
	//  21: LitString "tab-btn active"          — active class string (shared)
	//  22: LitString "tab-btn"                 — inactive class string (shared)
	//  23: OpCond [20, 21, 22]                 — button 0 class expr
	//  24: SignalGet "activeTab"                — for button 1 class
	//  25: LitInt "1"                          — comparison target
	//  26: OpEq [24, 25]                       — activeTab == 1
	//  27: OpCond [26, 21, 22]                 — button 1 class expr
	//  28: SignalGet "activeTab"                — for button 2 class
	//  29: LitInt "2"                          — comparison target
	//  30: OpEq [28, 29]                       — activeTab == 2
	//  31: OpCond [30, 21, 22]                 — button 2 class expr
	exprs := []Expr{
		{Op: OpLitInt, Value: "0", Type: TypeInt},                                                  // 0
		{Op: OpSignalGet, Value: "activeTab", Type: TypeInt},                                       // 1
		{Op: OpLitInt, Value: "0", Type: TypeInt},                                                  // 2
		{Op: OpEq, Operands: []ExprID{1, 2}, Type: TypeBool},                                      // 3
		{Op: OpLitString, Value: "About: GoSX is a Go-native web platform.", Type: TypeString},     // 4
		{Op: OpSignalGet, Value: "activeTab", Type: TypeInt},                                       // 5
		{Op: OpLitInt, Value: "1", Type: TypeInt},                                                  // 6
		{Op: OpEq, Operands: []ExprID{5, 6}, Type: TypeBool},                                      // 7
		{Op: OpLitString, Value: "Features: Server-first rendering, island hydration, signals.", Type: TypeString}, // 8
		{Op: OpLitString, Value: "Contact: github.com/odvcencio/gosx", Type: TypeString},           // 9
		{Op: OpCond, Operands: []ExprID{7, 8, 9}, Type: TypeString},                               // 10
		{Op: OpCond, Operands: []ExprID{3, 4, 10}, Type: TypeString},                              // 11
		{Op: OpSignalSet, Operands: []ExprID{13}, Value: "activeTab", Type: TypeInt},               // 12
		{Op: OpLitInt, Value: "0", Type: TypeInt},                                                  // 13
		{Op: OpSignalSet, Operands: []ExprID{15}, Value: "activeTab", Type: TypeInt},               // 14
		{Op: OpLitInt, Value: "1", Type: TypeInt},                                                  // 15
		{Op: OpSignalSet, Operands: []ExprID{17}, Value: "activeTab", Type: TypeInt},               // 16
		{Op: OpLitInt, Value: "2", Type: TypeInt},                                                  // 17
		// CSS class toggling — dynamic "tab-btn active" / "tab-btn" per button
		{Op: OpSignalGet, Value: "activeTab", Type: TypeInt},                                       // 18
		{Op: OpLitInt, Value: "0", Type: TypeInt},                                                  // 19
		{Op: OpEq, Operands: []ExprID{18, 19}, Type: TypeBool},                                    // 20
		{Op: OpLitString, Value: "tab-btn active", Type: TypeString},                               // 21
		{Op: OpLitString, Value: "tab-btn", Type: TypeString},                                      // 22
		{Op: OpCond, Operands: []ExprID{20, 21, 22}, Type: TypeString},                            // 23
		{Op: OpSignalGet, Value: "activeTab", Type: TypeInt},                                       // 24
		{Op: OpLitInt, Value: "1", Type: TypeInt},                                                  // 25
		{Op: OpEq, Operands: []ExprID{24, 25}, Type: TypeBool},                                    // 26
		{Op: OpCond, Operands: []ExprID{26, 21, 22}, Type: TypeString},                            // 27
		{Op: OpSignalGet, Value: "activeTab", Type: TypeInt},                                       // 28
		{Op: OpLitInt, Value: "2", Type: TypeInt},                                                  // 29
		{Op: OpEq, Operands: []ExprID{28, 29}, Type: TypeBool},                                    // 30
		{Op: OpCond, Operands: []ExprID{30, 21, 22}, Type: TypeString},                            // 31
	}

	// Nodes:
	//   0: div.tabs (root)
	//   1: div.tab-buttons
	//   2: button "About" [click -> showAbout, class -> expr[23]]
	//   3: button "Features" [click -> showFeatures, class -> expr[27]]
	//   4: button "Contact" [click -> showContact, class -> expr[31]]
	//   5: div.tab-content
	//   6: expr node (nested cond, expr[11])
	//   7: text "About"
	//   8: text "Features"
	//   9: text "Contact"
	nodes := []Node{
		{ // 0: div.tabs root
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "tabs"},
			},
			Children: []NodeID{1, 5},
		},
		{ // 1: div.tab-buttons
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "tab-buttons"},
			},
			Children: []NodeID{2, 3, 4},
		},
		{ // 2: button "About" — dynamic class via AttrExpr
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrExpr, Name: "class", Expr: ExprID(23)},
				{Kind: AttrEvent, Name: "click", Event: "showAbout"},
			},
			Children: []NodeID{7},
		},
		{ // 3: button "Features" — dynamic class via AttrExpr
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrExpr, Name: "class", Expr: ExprID(27)},
				{Kind: AttrEvent, Name: "click", Event: "showFeatures"},
			},
			Children: []NodeID{8},
		},
		{ // 4: button "Contact" — dynamic class via AttrExpr
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrExpr, Name: "class", Expr: ExprID(31)},
				{Kind: AttrEvent, Name: "click", Event: "showContact"},
			},
			Children: []NodeID{9},
		},
		{ // 5: div.tab-content
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "tab-content"},
			},
			Children: []NodeID{6},
		},
		{ // 6: expr node — nested conditional
			Kind: NodeExpr,
			Expr: ExprID(11),
		},
		{ // 7: text "About"
			Kind: NodeText,
			Text: "About",
		},
		{ // 8: text "Features"
			Kind: NodeText,
			Text: "Features",
		},
		{ // 9: text "Contact"
			Kind: NodeText,
			Text: "Contact",
		},
	}

	return &Program{
		Name:  "Tabs",
		Nodes: nodes,
		Root:  0,
		Exprs: exprs,
		Signals: []SignalDef{
			{Name: "activeTab", Type: TypeInt, Init: ExprID(0)},
		},
		Handlers: []Handler{
			{Name: "showAbout", Body: []ExprID{12}},
			{Name: "showFeatures", Body: []ExprID{14}},
			{Name: "showContact", Body: []ExprID{16}},
		},
		StaticMask: []bool{
			false, // 0: div.tabs (contains dynamic subtree)
			false, // 1: div.tab-buttons (contains dynamic attr children)
			false, // 2: button "About" (dynamic class)
			false, // 3: button "Features" (dynamic class)
			false, // 4: button "Contact" (dynamic class)
			false, // 5: div.tab-content (contains expr)
			false, // 6: expr node
			true,  // 7: text "About"
			true,  // 8: text "Features"
			true,  // 9: text "Contact"
		},
	}
}

// ToggleProgram returns a reference Program for a show/hide toggle component.
//
// One signal "visible" (TypeBool, init false). Two handlers: "toggle" (click)
// and "toggleKey" (keydown) — both negate the signal via OpNot. The keyboard
// handler demonstrates that islands can respond to keyboard events, not just
// clicks.
func ToggleProgram() *Program {
	// Expressions:
	//   0: LitBool "false"                     — initial value for visible
	//   1: SignalGet "visible"                  — read visible (for display)
	//   2: LitString "This content is now visible!" — shown when visible
	//   3: LitString ""                         — shown when hidden
	//   4: OpCond [1, 2, 3]                     — if visible: text, else: ""
	//   5: SignalSet "visible" <- [7]            — set visible = !visible (click)
	//   6: SignalGet "visible"                   — read visible (for not)
	//   7: OpNot [6]                             — !visible
	//   8: SignalSet "visible" <- [10]           — set visible = !visible (keydown)
	//   9: SignalGet "visible"                   — read visible (for not, keydown)
	//  10: OpNot [9]                             — !visible (keydown)
	exprs := []Expr{
		{Op: OpLitBool, Value: "false", Type: TypeBool},                               // 0
		{Op: OpSignalGet, Value: "visible", Type: TypeBool},                           // 1
		{Op: OpLitString, Value: "This content is now visible!", Type: TypeString},     // 2
		{Op: OpLitString, Value: "", Type: TypeString},                                // 3
		{Op: OpCond, Operands: []ExprID{1, 2, 3}, Type: TypeString},                  // 4
		{Op: OpSignalSet, Operands: []ExprID{7}, Value: "visible", Type: TypeBool},    // 5
		{Op: OpSignalGet, Value: "visible", Type: TypeBool},                           // 6
		{Op: OpNot, Operands: []ExprID{6}, Type: TypeBool},                            // 7
		{Op: OpSignalSet, Operands: []ExprID{10}, Value: "visible", Type: TypeBool},   // 8
		{Op: OpSignalGet, Value: "visible", Type: TypeBool},                           // 9
		{Op: OpNot, Operands: []ExprID{9}, Type: TypeBool},                            // 10
	}

	// Nodes:
	//   0: div.toggle (root)
	//   1: button "Toggle Content" [click -> toggle, keydown -> toggleKey]
	//   2: p (wrapper for conditional content)
	//   3: expr node (cond, expr[4])
	//   4: text "Toggle Content"
	nodes := []Node{
		{ // 0: div.toggle root
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "toggle"},
			},
			Children: []NodeID{1, 2},
		},
		{ // 1: button "Toggle Content" — click and keydown handlers
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrEvent, Name: "click", Event: "toggle"},
				{Kind: AttrEvent, Name: "keydown", Event: "toggleKey"},
			},
			Children: []NodeID{4},
		},
		{ // 2: p wrapper for conditional content
			Kind: NodeElement,
			Tag:  "p",
			Children: []NodeID{3},
		},
		{ // 3: expr node — conditional content
			Kind: NodeExpr,
			Expr: ExprID(4),
		},
		{ // 4: text "Toggle Content"
			Kind: NodeText,
			Text: "Toggle Content",
		},
	}

	return &Program{
		Name:  "Toggle",
		Nodes: nodes,
		Root:  0,
		Exprs: exprs,
		Signals: []SignalDef{
			{Name: "visible", Type: TypeBool, Init: ExprID(0)},
		},
		Handlers: []Handler{
			{Name: "toggle", Body: []ExprID{5}},
			{Name: "toggleKey", Body: []ExprID{8}},
		},
		StaticMask: []bool{
			false, // 0: div.toggle (contains dynamic subtree)
			true,  // 1: button
			false, // 2: p wrapper (contains expr)
			false, // 3: expr node
			true,  // 4: text "Toggle Content"
		},
	}
}

// TodoProgram returns a reference Program for a simplified todo list component.
//
// Two signals: "items" (TypeString, comma-separated list), "input" (TypeString).
// Three handlers: updateInput (appends "a"), addItem (concatenates input to
// items), clearAll (resets items to "").
func TodoProgram() *Program {
	// Expressions:
	//   0: LitString ""                        — initial value for items
	//   1: LitString ""                        — initial value for input
	//   2: SignalGet "input"                    — read input (for display)
	//   3: SignalGet "items"                    — read items (for display)
	//   4: SignalSet "input" <- [6]             — updateInput: input = input + "a"
	//   5: SignalGet "input"                    — read input (for concat)
	//   6: OpConcat [5, 7]                      — input + "a"
	//   7: LitString "a"                        — literal "a"
	//   8: SignalSet "items" <- [12]            — addItem: items = concat(items, ",", input)
	//   9: SignalSet "input" <- [10]            — addItem: input = ""
	//  10: LitString ""                         — literal ""
	//  11: SignalGet "items"                    — read items (for concat)
	//  12: OpConcat [11, 13, 14]                — items + "," + input
	//  13: LitString ","                        — literal ","
	//  14: SignalGet "input"                    — read input (for concat)
	//  15: SignalSet "items" <- [16]            — clearAll: items = ""
	//  16: LitString ""                         — literal ""
	exprs := []Expr{
		{Op: OpLitString, Value: "", Type: TypeString},                                   // 0
		{Op: OpLitString, Value: "", Type: TypeString},                                   // 1
		{Op: OpSignalGet, Value: "input", Type: TypeString},                              // 2
		{Op: OpSignalGet, Value: "items", Type: TypeString},                              // 3
		{Op: OpSignalSet, Operands: []ExprID{6}, Value: "input", Type: TypeString},       // 4
		{Op: OpSignalGet, Value: "input", Type: TypeString},                              // 5
		{Op: OpConcat, Operands: []ExprID{5, 7}, Type: TypeString},                      // 6
		{Op: OpLitString, Value: "a", Type: TypeString},                                  // 7
		{Op: OpSignalSet, Operands: []ExprID{12}, Value: "items", Type: TypeString},      // 8
		{Op: OpSignalSet, Operands: []ExprID{10}, Value: "input", Type: TypeString},      // 9
		{Op: OpLitString, Value: "", Type: TypeString},                                   // 10
		{Op: OpSignalGet, Value: "items", Type: TypeString},                              // 11
		{Op: OpConcat, Operands: []ExprID{11, 13, 14}, Type: TypeString},                // 12
		{Op: OpLitString, Value: ",", Type: TypeString},                                  // 13
		{Op: OpSignalGet, Value: "input", Type: TypeString},                              // 14
		{Op: OpSignalSet, Operands: []ExprID{16}, Value: "items", Type: TypeString},      // 15
		{Op: OpLitString, Value: "", Type: TypeString},                                   // 16
	}

	// Nodes:
	//   0: div.todo (root)
	//   1: h3 "Todo List"
	//   2: div.todo-input
	//   3: span (expr showing input)
	//   4: button "Add" [click -> addItem]
	//   5: div.todo-items
	//   6: expr node (items display, expr[3])
	//   7: button "Clear All" [click -> clearAll]
	//   8: text "Todo List"
	//   9: text "Add"
	//  10: text "Clear All"
	nodes := []Node{
		{ // 0: div.todo root
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "todo"},
			},
			Children: []NodeID{1, 2, 5, 7},
		},
		{ // 1: h3 "Todo List"
			Kind:     NodeElement,
			Tag:      "h3",
			Children: []NodeID{8},
		},
		{ // 2: div.todo-input
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "todo-input"},
			},
			Children: []NodeID{3, 4},
		},
		{ // 3: span showing input expr
			Kind: NodeExpr,
			Expr: ExprID(2),
		},
		{ // 4: button "Add"
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrEvent, Name: "click", Event: "addItem"},
			},
			Children: []NodeID{9},
		},
		{ // 5: div.todo-items
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "todo-items"},
			},
			Children: []NodeID{6},
		},
		{ // 6: expr node — items display
			Kind: NodeExpr,
			Expr: ExprID(3),
		},
		{ // 7: button "Clear All"
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrEvent, Name: "click", Event: "clearAll"},
			},
			Children: []NodeID{10},
		},
		{ // 8: text "Todo List"
			Kind: NodeText,
			Text: "Todo List",
		},
		{ // 9: text "Add"
			Kind: NodeText,
			Text: "Add",
		},
		{ // 10: text "Clear All"
			Kind: NodeText,
			Text: "Clear All",
		},
	}

	return &Program{
		Name:  "Todo",
		Nodes: nodes,
		Root:  0,
		Exprs: exprs,
		Signals: []SignalDef{
			{Name: "items", Type: TypeString, Init: ExprID(0)},
			{Name: "input", Type: TypeString, Init: ExprID(1)},
		},
		Handlers: []Handler{
			{Name: "updateInput", Body: []ExprID{4}},
			{Name: "addItem", Body: []ExprID{8, 9}},
			{Name: "clearAll", Body: []ExprID{15}},
		},
		StaticMask: []bool{
			false, // 0: div.todo (contains dynamic subtree)
			true,  // 1: h3
			false, // 2: div.todo-input (contains expr)
			false, // 3: expr node (input display)
			true,  // 4: button "Add"
			false, // 5: div.todo-items (contains expr)
			false, // 6: expr node (items display)
			true,  // 7: button "Clear All"
			true,  // 8: text "Todo List"
			true,  // 9: text "Add"
			true,  // 10: text "Clear All"
		},
	}
}

// FormProgram returns a reference Program for a form with two-way input binding.
//
// Two signals: "name" (TypeString, init ""), "valid" (TypeBool, init false).
// Three handlers: updateName (reads OpEventGet "value" from input event —
// true two-way binding), fillName (button toggle for "Alice"/""), validateForm
// (sets valid to name != "").
func FormProgram() *Program {
	// Expressions:
	//   0: LitString ""                        — initial value for name
	//   1: LitBool "false"                     — initial value for valid
	//   2: SignalGet "name"                     — read name (for display)
	//   3: SignalGet "valid"                    — read valid (for cond)
	//   4: LitString "Form is valid ✓"         — shown when valid
	//   5: LitString "Please fill in name"     — shown when invalid
	//   6: OpCond [3, 4, 5]                     — if valid: "Form is valid" else: "Please fill in"
	//   7: OpEventGet "value"                   — read input value from event data
	//   8: SignalSet "name" <- [7]              — updateName: name = event.value (two-way binding)
	//   9: SignalSet "name" <- [13]             — fillName: name = cond(name=="" , "Alice", "")
	//  10: SignalGet "name"                     — read name (for comparison)
	//  11: LitString ""                         — literal ""
	//  12: OpEq [10, 11]                        — name == ""
	//  13: OpCond [12, 14, 15]                  — name=="" ? "Alice" : ""
	//  14: LitString "Alice"                    — literal "Alice"
	//  15: LitString ""                         — literal ""
	//  16: SignalSet "valid" <- [19]            — validateForm: valid = name != ""
	//  17: SignalGet "name"                     — read name (for neq)
	//  18: LitString ""                         — literal ""
	//  19: OpNeq [17, 18]                       — name != ""
	exprs := []Expr{
		{Op: OpLitString, Value: "", Type: TypeString},                                     // 0
		{Op: OpLitBool, Value: "false", Type: TypeBool},                                    // 1
		{Op: OpSignalGet, Value: "name", Type: TypeString},                                 // 2
		{Op: OpSignalGet, Value: "valid", Type: TypeBool},                                  // 3
		{Op: OpLitString, Value: "Form is valid \u2713", Type: TypeString},                 // 4
		{Op: OpLitString, Value: "Please fill in name", Type: TypeString},                  // 5
		{Op: OpCond, Operands: []ExprID{3, 4, 5}, Type: TypeString},                       // 6
		{Op: OpEventGet, Value: "value", Type: TypeString},                                 // 7
		{Op: OpSignalSet, Operands: []ExprID{7}, Value: "name", Type: TypeString},          // 8
		{Op: OpSignalSet, Operands: []ExprID{13}, Value: "name", Type: TypeString},         // 9
		{Op: OpSignalGet, Value: "name", Type: TypeString},                                 // 10
		{Op: OpLitString, Value: "", Type: TypeString},                                     // 11
		{Op: OpEq, Operands: []ExprID{10, 11}, Type: TypeBool},                            // 12
		{Op: OpCond, Operands: []ExprID{12, 14, 15}, Type: TypeString},                    // 13
		{Op: OpLitString, Value: "Alice", Type: TypeString},                                // 14
		{Op: OpLitString, Value: "", Type: TypeString},                                     // 15
		{Op: OpSignalSet, Operands: []ExprID{19}, Value: "valid", Type: TypeBool},          // 16
		{Op: OpSignalGet, Value: "name", Type: TypeString},                                 // 17
		{Op: OpLitString, Value: "", Type: TypeString},                                     // 18
		{Op: OpNeq, Operands: []ExprID{17, 18}, Type: TypeBool},                           // 19
	}

	// Nodes:
	//   0: div.form-demo (root)
	//   1: h3 "Form Validation"
	//   2: div.form-field
	//   3: label "Name"
	//   4: input [type=text, input -> updateName] — two-way binding
	//   5: button "Fill Name" [click -> fillName]
	//   6: span.field-value (expr showing name)
	//   7: div.form-status
	//   8: expr node (valid cond, expr[6])
	//   9: button "Validate" [click -> validateForm]
	//  10: text "Form Validation"
	//  11: text "Name"
	//  12: text "Fill Name"
	//  13: text "Validate"
	//  14: expr node — name display (inline in span)
	nodes := []Node{
		{ // 0: div.form-demo root
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "form-demo"},
			},
			Children: []NodeID{1, 2, 7, 9},
		},
		{ // 1: h3 "Form Validation"
			Kind:     NodeElement,
			Tag:      "h3",
			Children: []NodeID{10},
		},
		{ // 2: div.form-field
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "form-field"},
			},
			Children: []NodeID{3, 4, 5, 6},
		},
		{ // 3: label "Name"
			Kind:     NodeElement,
			Tag:      "label",
			Children: []NodeID{11},
		},
		{ // 4: input — two-way binding via OpEventGet
			Kind: NodeElement,
			Tag:  "input",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "type", Value: "text"},
				{Kind: AttrStatic, Name: "placeholder", Value: "Enter name..."},
				{Kind: AttrEvent, Name: "input", Event: "updateName"},
			},
		},
		{ // 5: button "Fill Name"
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrEvent, Name: "click", Event: "fillName"},
			},
			Children: []NodeID{12},
		},
		{ // 6: span.field-value (expr showing name)
			Kind: NodeElement,
			Tag:  "span",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "field-value"},
			},
			Children: []NodeID{14},
		},
		{ // 7: div.form-status
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "form-status"},
			},
			Children: []NodeID{8},
		},
		{ // 8: expr node — valid conditional
			Kind: NodeExpr,
			Expr: ExprID(6),
		},
		{ // 9: button "Validate"
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrEvent, Name: "click", Event: "validateForm"},
			},
			Children: []NodeID{13},
		},
		{ // 10: text "Form Validation"
			Kind: NodeText,
			Text: "Form Validation",
		},
		{ // 11: text "Name"
			Kind: NodeText,
			Text: "Name",
		},
		{ // 12: text "Fill Name"
			Kind: NodeText,
			Text: "Fill Name",
		},
		{ // 13: text "Validate"
			Kind: NodeText,
			Text: "Validate",
		},
		{ // 14: expr node — name display (inline in span)
			Kind: NodeExpr,
			Expr: ExprID(2),
		},
	}

	return &Program{
		Name:  "Form",
		Nodes: nodes,
		Root:  0,
		Exprs: exprs,
		Signals: []SignalDef{
			{Name: "name", Type: TypeString, Init: ExprID(0)},
			{Name: "valid", Type: TypeBool, Init: ExprID(1)},
		},
		Handlers: []Handler{
			{Name: "updateName", Body: []ExprID{8}},
			{Name: "fillName", Body: []ExprID{9}},
			{Name: "validateForm", Body: []ExprID{16}},
		},
		StaticMask: []bool{
			false, // 0: div.form-demo (contains dynamic subtree)
			true,  // 1: h3
			false, // 2: div.form-field (contains expr)
			true,  // 3: label
			true,  // 4: input (static element, value via events)
			true,  // 5: button "Fill Name"
			false, // 6: span.field-value (contains expr)
			false, // 7: div.form-status (contains expr)
			false, // 8: expr node (valid cond)
			true,  // 9: button "Validate"
			true,  // 10: text "Form Validation"
			true,  // 11: text "Name"
			true,  // 12: text "Fill Name"
			true,  // 13: text "Validate"
			false, // 14: expr node (name display)
		},
	}
}

// DerivedProgram returns a reference Program for a multi-signal price calculator.
//
// Three signals: "price" (TypeInt, init 100), "quantity" (TypeInt, init 1),
// "discount" (TypeInt, init 0). Two handlers: incQuantity (quantity + 1),
// toggleDiscount (discount == 0 ? 10 : 0). Displays a computed total:
// price * quantity - discount.
func DerivedProgram() *Program {
	// Expressions:
	//   0: LitInt "100"                         — initial value for price
	//   1: LitInt "1"                           — initial value for quantity
	//   2: LitInt "0"                           — initial value for discount
	//   3: SignalGet "price"                     — read price (for display)
	//   4: SignalGet "quantity"                  — read quantity (for display)
	//   5: SignalGet "discount"                  — read discount (for display)
	//   6: SignalGet "price"                     — read price (for total)
	//   7: SignalGet "quantity"                  — read quantity (for total)
	//   8: OpMul [6, 7]                         — price * quantity
	//   9: SignalGet "discount"                  — read discount (for total)
	//  10: OpSub [8, 9]                         — (price * quantity) - discount
	//  11: SignalSet "quantity" <- [14]          — incQuantity: quantity = quantity + 1
	//  12: SignalGet "quantity"                  — read quantity (for add)
	//  13: LitInt "1"                           — literal 1
	//  14: OpAdd [12, 13]                       — quantity + 1
	//  15: SignalSet "discount" <- [20]         — toggleDiscount: discount = cond(discount==0, 10, 0)
	//  16: SignalGet "discount"                  — read discount (for comparison)
	//  17: LitInt "0"                           — literal 0
	//  18: OpEq [16, 17]                        — discount == 0
	//  19: LitInt "10"                          — literal 10
	//  20: OpCond [18, 19, 17]                  — discount==0 ? 10 : 0 (reuses expr[17])
	exprs := []Expr{
		{Op: OpLitInt, Value: "100", Type: TypeInt},                                     // 0
		{Op: OpLitInt, Value: "1", Type: TypeInt},                                       // 1
		{Op: OpLitInt, Value: "0", Type: TypeInt},                                       // 2
		{Op: OpSignalGet, Value: "price", Type: TypeInt},                                // 3
		{Op: OpSignalGet, Value: "quantity", Type: TypeInt},                             // 4
		{Op: OpSignalGet, Value: "discount", Type: TypeInt},                             // 5
		{Op: OpSignalGet, Value: "price", Type: TypeInt},                                // 6
		{Op: OpSignalGet, Value: "quantity", Type: TypeInt},                             // 7
		{Op: OpMul, Operands: []ExprID{6, 7}, Type: TypeInt},                           // 8
		{Op: OpSignalGet, Value: "discount", Type: TypeInt},                             // 9
		{Op: OpSub, Operands: []ExprID{8, 9}, Type: TypeInt},                           // 10
		{Op: OpSignalSet, Operands: []ExprID{14}, Value: "quantity", Type: TypeInt},     // 11
		{Op: OpSignalGet, Value: "quantity", Type: TypeInt},                             // 12
		{Op: OpLitInt, Value: "1", Type: TypeInt},                                       // 13
		{Op: OpAdd, Operands: []ExprID{12, 13}, Type: TypeInt},                         // 14
		{Op: OpSignalSet, Operands: []ExprID{20}, Value: "discount", Type: TypeInt},     // 15
		{Op: OpSignalGet, Value: "discount", Type: TypeInt},                             // 16
		{Op: OpLitInt, Value: "0", Type: TypeInt},                                       // 17
		{Op: OpEq, Operands: []ExprID{16, 17}, Type: TypeBool},                         // 18
		{Op: OpLitInt, Value: "10", Type: TypeInt},                                      // 19
		{Op: OpCond, Operands: []ExprID{18, 19, 17}, Type: TypeInt},                    // 20
	}

	// Nodes:
	//   0: div.derived (root)
	//   1: h3 "Price Calculator"
	//   2: div.row — "Price: $" + expr(price)
	//   3: text "Price: $"
	//   4: expr node (price, expr[3])
	//   5: div.row — "Qty: " + expr(quantity) + button "+"
	//   6: text "Qty: "
	//   7: expr node (quantity, expr[4])
	//   8: button "+" [click -> incQuantity]
	//   9: div.row — "Discount: $" + expr(discount) + button "Toggle 10% off"
	//  10: text "Discount: $"
	//  11: expr node (discount, expr[5])
	//  12: button "Toggle 10% off" [click -> toggleDiscount]
	//  13: div.total — "Total: $" + expr(total)
	//  14: text "Total: $"
	//  15: expr node (total, expr[10])
	//  16: text "Price Calculator"
	//  17: text "+"
	//  18: text "Toggle 10% off"
	nodes := []Node{
		{ // 0: div.derived root
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "derived"},
			},
			Children: []NodeID{1, 2, 5, 9, 13},
		},
		{ // 1: h3 "Price Calculator"
			Kind:     NodeElement,
			Tag:      "h3",
			Children: []NodeID{16},
		},
		{ // 2: div.row (price)
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "row"},
			},
			Children: []NodeID{3, 4},
		},
		{ // 3: text "Price: $"
			Kind: NodeText,
			Text: "Price: $",
		},
		{ // 4: expr node — price display
			Kind: NodeExpr,
			Expr: ExprID(3),
		},
		{ // 5: div.row (quantity)
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "row"},
			},
			Children: []NodeID{6, 7, 8},
		},
		{ // 6: text "Qty: "
			Kind: NodeText,
			Text: "Qty: ",
		},
		{ // 7: expr node — quantity display
			Kind: NodeExpr,
			Expr: ExprID(4),
		},
		{ // 8: button "+"
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrEvent, Name: "click", Event: "incQuantity"},
			},
			Children: []NodeID{17},
		},
		{ // 9: div.row (discount)
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "row"},
			},
			Children: []NodeID{10, 11, 12},
		},
		{ // 10: text "Discount: $"
			Kind: NodeText,
			Text: "Discount: $",
		},
		{ // 11: expr node — discount display
			Kind: NodeExpr,
			Expr: ExprID(5),
		},
		{ // 12: button "Toggle 10% off"
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrEvent, Name: "click", Event: "toggleDiscount"},
			},
			Children: []NodeID{18},
		},
		{ // 13: div.total
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "total"},
			},
			Children: []NodeID{14, 15},
		},
		{ // 14: text "Total: $"
			Kind: NodeText,
			Text: "Total: $",
		},
		{ // 15: expr node — total display
			Kind: NodeExpr,
			Expr: ExprID(10),
		},
		{ // 16: text "Price Calculator"
			Kind: NodeText,
			Text: "Price Calculator",
		},
		{ // 17: text "+"
			Kind: NodeText,
			Text: "+",
		},
		{ // 18: text "Toggle 10% off"
			Kind: NodeText,
			Text: "Toggle 10% off",
		},
	}

	return &Program{
		Name:  "Derived",
		Nodes: nodes,
		Root:  0,
		Exprs: exprs,
		Signals: []SignalDef{
			{Name: "price", Type: TypeInt, Init: ExprID(0)},
			{Name: "quantity", Type: TypeInt, Init: ExprID(1)},
			{Name: "discount", Type: TypeInt, Init: ExprID(2)},
		},
		Handlers: []Handler{
			{Name: "incQuantity", Body: []ExprID{11}},
			{Name: "toggleDiscount", Body: []ExprID{15}},
		},
		StaticMask: []bool{
			false, // 0: div.derived (contains dynamic subtree)
			true,  // 1: h3
			false, // 2: div.row (price, contains expr)
			true,  // 3: text "Price: $"
			false, // 4: expr node (price)
			false, // 5: div.row (quantity, contains expr)
			true,  // 6: text "Qty: "
			false, // 7: expr node (quantity)
			true,  // 8: button "+"
			false, // 9: div.row (discount, contains expr)
			true,  // 10: text "Discount: $"
			false, // 11: expr node (discount)
			true,  // 12: button "Toggle 10% off"
			false, // 13: div.total (contains expr)
			true,  // 14: text "Total: $"
			false, // 15: expr node (total)
			true,  // 16: text "Price Calculator"
			true,  // 17: text "+"
			true,  // 18: text "Toggle 10% off"
		},
	}
}

// CounterProgram returns a reference Program for a Counter component.
//
// The counter has a single "count" signal, two buttons (decrement/increment),
// and an expression node displaying the current count. This serves as a
// canonical test fixture and reference implementation.
func CounterProgram() *Program {
	// Expressions:
	//   0: SignalGet "count"       — reads current count (for display)
	//   1: LitInt "0"              — initial value for count signal
	//   2: SignalSet "count" <- [4] — decrement: count = count - 1
	//   3: SignalSet "count" <- [5] — increment: count = count + 1
	//   4: Sub [6, 7]              — count - 1
	//   5: Add [8, 9]              — count + 1
	//   6: SignalGet "count"       — reads count (for sub)
	//   7: LitInt "1"              — literal 1 (for sub)
	//   8: SignalGet "count"       — reads count (for add)
	//   9: LitInt "1"              — literal 1 (for add)
	exprs := []Expr{
		{Op: OpSignalGet, Value: "count", Type: TypeInt},                          // 0
		{Op: OpLitInt, Value: "0", Type: TypeInt},                                 // 1
		{Op: OpSignalSet, Operands: []ExprID{4}, Value: "count", Type: TypeInt},   // 2
		{Op: OpSignalSet, Operands: []ExprID{5}, Value: "count", Type: TypeInt},   // 3
		{Op: OpSub, Operands: []ExprID{6, 7}, Type: TypeInt},                     // 4
		{Op: OpAdd, Operands: []ExprID{8, 9}, Type: TypeInt},                     // 5
		{Op: OpSignalGet, Value: "count", Type: TypeInt},                          // 6
		{Op: OpLitInt, Value: "1", Type: TypeInt},                                 // 7
		{Op: OpSignalGet, Value: "count", Type: TypeInt},                          // 8
		{Op: OpLitInt, Value: "1", Type: TypeInt},                                 // 9
	}

	// Nodes:
	//   0: div.counter (root element)
	//   1: button "-" with click->decrement
	//   2: expr node displaying count (expr[0])
	//   3: button "+" with click->increment
	//   4: text "-"
	//   5: text "+"
	nodes := []Node{
		{ // 0: div.counter root
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "counter"},
			},
			Children: []NodeID{1, 2, 3},
		},
		{ // 1: button "-" (decrement)
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrEvent, Name: "click", Event: "decrement"},
			},
			Children: []NodeID{4},
		},
		{ // 2: expr node showing count
			Kind: NodeExpr,
			Expr: ExprID(0),
		},
		{ // 3: button "+" (increment)
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrEvent, Name: "click", Event: "increment"},
			},
			Children: []NodeID{5},
		},
		{ // 4: text "-"
			Kind: NodeText,
			Text: "-",
		},
		{ // 5: text "+"
			Kind: NodeText,
			Text: "+",
		},
	}

	return &Program{
		Name:  "Counter",
		Nodes: nodes,
		Root:  0,
		Exprs: exprs,
		Signals: []SignalDef{
			{Name: "count", Type: TypeInt, Init: ExprID(1)},
		},
		Handlers: []Handler{
			{Name: "decrement", Body: []ExprID{2}},
			{Name: "increment", Body: []ExprID{3}},
		},
		StaticMask: []bool{false, true, false, true, true, true},
	}
}

// EditorProgram returns a code editor island program.
//
// The editor has a textarea for input, a live character count, and a clear button.
// On every input event, the code signal is set to the textarea value via OpEventGet.
func EditorProgram() *Program {
	// Expressions:
	//   0: SignalGet "code"          — current code content (for display/binding)
	//   1: LitString ""              — initial empty code
	//   2: EventGet "value"          — reads textarea value from input event
	//   3: SignalSet "code" <- [2]   — set code to event value (onInput handler)
	//   4: LitString ""              — empty string for clear
	//   5: SignalSet "code" <- [4]   — clear code (clear handler)
	//   6: OpLen [7]                 — character count
	//   7: SignalGet "code"          — code for len()
	exprs := []Expr{
		{Op: OpSignalGet, Value: "code", Type: TypeString},                        // 0
		{Op: OpLitString, Value: "", Type: TypeString},                            // 1
		{Op: OpEventGet, Value: "value", Type: TypeString},                        // 2
		{Op: OpSignalSet, Operands: []ExprID{2}, Value: "code", Type: TypeString}, // 3
		{Op: OpLitString, Value: "", Type: TypeString},                            // 4
		{Op: OpSignalSet, Operands: []ExprID{4}, Value: "code", Type: TypeString}, // 5
		{Op: OpLen, Operands: []ExprID{7}, Type: TypeInt},                         // 6
		{Op: OpSignalGet, Value: "code", Type: TypeString},                        // 7
	}

	// Nodes:
	//   0: div.editor (root)
	//   1: div.editor-header
	//   2: h3 "Code Editor"
	//   3: span.char-count — displays character count
	//   4: expr: char count (expr[6])
	//   5: text " chars"
	//   6: textarea (with input event -> onInput)
	//   7: div.editor-actions
	//   8: button "Clear" (click -> clear)
	//   9: div.editor-preview
	//   10: pre — displays code content
	//   11: expr: code content (expr[0])
	nodes := []Node{
		{ // 0: root
			Kind: NodeElement, Tag: "div",
			Attrs:    []Attr{{Kind: AttrStatic, Name: "class", Value: "editor"}},
			Children: []NodeID{1, 6, 7, 9},
		},
		{ // 1: header
			Kind: NodeElement, Tag: "div",
			Attrs:    []Attr{{Kind: AttrStatic, Name: "class", Value: "editor-header"}},
			Children: []NodeID{2, 3},
		},
		{ // 2: title
			Kind: NodeText, Text: "Code Editor",
		},
		{ // 3: char count span
			Kind: NodeElement, Tag: "span",
			Attrs:    []Attr{{Kind: AttrStatic, Name: "class", Value: "char-count"}},
			Children: []NodeID{4, 5},
		},
		{ // 4: char count value (expr)
			Kind: NodeExpr, Expr: ExprID(6),
		},
		{ // 5: " chars" label
			Kind: NodeText, Text: " chars",
		},
		{ // 6: textarea
			Kind: NodeElement, Tag: "textarea",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "editor-textarea"},
				{Kind: AttrStatic, Name: "rows", Value: "12"},
				{Kind: AttrStatic, Name: "placeholder", Value: "Type or paste code here..."},
				{Kind: AttrEvent, Name: "input", Event: "onInput"},
			},
			Children: []NodeID{},
		},
		{ // 7: actions
			Kind: NodeElement, Tag: "div",
			Attrs:    []Attr{{Kind: AttrStatic, Name: "class", Value: "editor-actions"}},
			Children: []NodeID{8},
		},
		{ // 8: clear button
			Kind: NodeElement, Tag: "button",
			Attrs: []Attr{
				{Kind: AttrEvent, Name: "click", Event: "clear"},
			},
			Children: []NodeID{},
		},
		{ // 9: preview section
			Kind: NodeElement, Tag: "div",
			Attrs:    []Attr{{Kind: AttrStatic, Name: "class", Value: "editor-preview"}},
			Children: []NodeID{10},
		},
		{ // 10: pre element for code display
			Kind: NodeElement, Tag: "pre",
			Attrs:    []Attr{{Kind: AttrStatic, Name: "class", Value: "code-output"}},
			Children: []NodeID{11},
		},
		{ // 11: code content (expr)
			Kind: NodeExpr, Expr: ExprID(0),
		},
	}

	return &Program{
		Name:  "Editor",
		Nodes: nodes,
		Root:  0,
		Exprs: exprs,
		Signals: []SignalDef{
			{Name: "code", Type: TypeString, Init: ExprID(1)},
		},
		Handlers: []Handler{
			{Name: "onInput", Body: []ExprID{3}},
			{Name: "clear", Body: []ExprID{5}},
		},
		StaticMask: []bool{
			false, // 0: root (contains dynamic children)
			false, // 1: header (contains char count)
			true,  // 2: title text
			false, // 3: char count span
			false, // 4: char count expr
			true,  // 5: " chars" text
			true,  // 6: textarea (static element, value handled by events)
			true,  // 7: actions div
			true,  // 8: clear button
			false, // 9: preview
			false, // 10: pre
			false, // 11: code expr
		},
	}
}

// ListProgram returns a reference Program for a dynamic list rendering component.
//
// Demonstrates adding/removing items from a list — a core SPA pattern. Items
// are stored as a comma-separated string (the VM has no array type). Three
// signals: "items" (TypeString), "input" (TypeString), "count" (TypeInt).
//
// Handlers:
//   - addItem: reads OpEventGet("value"), concatenates to items, increments count
//   - removeLastItem: decrements count (simplified — no string truncation)
//   - clearItems: sets items to "", count to 0
func ListProgram() *Program {
	// Expressions:
	//   0: LitString ""                       — initial value for items
	//   1: LitString ""                       — initial value for input
	//   2: LitInt "0"                         — initial value for count
	//   3: SignalGet "items"                   — read items (for display)
	//   4: SignalGet "count"                   — read count (for display)
	//
	// addItem handler:
	//   5: OpEventGet "value"                  — read input value from event
	//   6: SignalGet "items"                   — current items
	//   7: LitString ","                       — separator
	//   8: OpConcat [6, 7]                     — items + ","
	//   9: OpConcat [8, 5]                     — items + "," + eventValue
	//  10: SignalSet "items" <- [9]            — items = items + "," + eventValue
	//  11: SignalGet "count"                   — read count (for increment)
	//  12: LitInt "1"                          — literal 1
	//  13: OpAdd [11, 12]                      — count + 1
	//  14: SignalSet "count" <- [13]           — count = count + 1
	//
	// removeLastItem handler:
	//  15: SignalGet "count"                   — read count (for decrement)
	//  16: LitInt "1"                          — literal 1
	//  17: OpSub [15, 16]                      — count - 1
	//  18: SignalSet "count" <- [17]           — count = count - 1
	//
	// clearItems handler:
	//  19: LitString ""                       — empty string
	//  20: SignalSet "items" <- [19]           — items = ""
	//  21: LitInt "0"                          — literal 0
	//  22: SignalSet "count" <- [21]           — count = 0
	exprs := []Expr{
		{Op: OpLitString, Value: "", Type: TypeString},                                    // 0
		{Op: OpLitString, Value: "", Type: TypeString},                                    // 1
		{Op: OpLitInt, Value: "0", Type: TypeInt},                                         // 2
		{Op: OpSignalGet, Value: "items", Type: TypeString},                               // 3
		{Op: OpSignalGet, Value: "count", Type: TypeInt},                                  // 4
		// addItem handler
		{Op: OpEventGet, Value: "value", Type: TypeString},                                // 5
		{Op: OpSignalGet, Value: "items", Type: TypeString},                               // 6
		{Op: OpLitString, Value: ",", Type: TypeString},                                   // 7
		{Op: OpConcat, Operands: []ExprID{6, 7}, Type: TypeString},                       // 8
		{Op: OpConcat, Operands: []ExprID{8, 5}, Type: TypeString},                       // 9
		{Op: OpSignalSet, Operands: []ExprID{9}, Value: "items", Type: TypeString},        // 10
		{Op: OpSignalGet, Value: "count", Type: TypeInt},                                  // 11
		{Op: OpLitInt, Value: "1", Type: TypeInt},                                         // 12
		{Op: OpAdd, Operands: []ExprID{11, 12}, Type: TypeInt},                           // 13
		{Op: OpSignalSet, Operands: []ExprID{13}, Value: "count", Type: TypeInt},          // 14
		// removeLastItem handler
		{Op: OpSignalGet, Value: "count", Type: TypeInt},                                  // 15
		{Op: OpLitInt, Value: "1", Type: TypeInt},                                         // 16
		{Op: OpSub, Operands: []ExprID{15, 16}, Type: TypeInt},                           // 17
		{Op: OpSignalSet, Operands: []ExprID{17}, Value: "count", Type: TypeInt},          // 18
		// clearItems handler
		{Op: OpLitString, Value: "", Type: TypeString},                                    // 19
		{Op: OpSignalSet, Operands: []ExprID{19}, Value: "items", Type: TypeString},       // 20
		{Op: OpLitInt, Value: "0", Type: TypeInt},                                         // 21
		{Op: OpSignalSet, Operands: []ExprID{21}, Value: "count", Type: TypeInt},          // 22
	}

	// Nodes:
	//   0: div.list-demo (root)
	//   1: div.list-input
	//   2: input [type=text, placeholder, input -> addItem]
	//   3: button "Add" [click -> addItem]
	//   4: div.list-display
	//   5: span.item-count — contains count expr + " items" text
	//   6: expr node — count display (expr[4])
	//   7: text " items"
	//   8: pre.item-list (expr: items)
	//   9: expr node — items display (expr[3])
	//  10: div.list-actions
	//  11: button "Remove Last" [click -> removeLastItem]
	//  12: button "Clear All" [click -> clearItems]
	//  13: text "Add"
	//  14: text "Remove Last"
	//  15: text "Clear All"
	nodes := []Node{
		{ // 0: div.list-demo root
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "list-demo"},
			},
			Children: []NodeID{1, 4, 10},
		},
		{ // 1: div.list-input
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "list-input"},
			},
			Children: []NodeID{2, 3},
		},
		{ // 2: input — add item via event value
			Kind: NodeElement,
			Tag:  "input",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "type", Value: "text"},
				{Kind: AttrStatic, Name: "placeholder", Value: "Add item..."},
				{Kind: AttrEvent, Name: "input", Event: "addItem"},
			},
		},
		{ // 3: button "Add"
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrEvent, Name: "click", Event: "addItem"},
			},
			Children: []NodeID{13},
		},
		{ // 4: div.list-display
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "list-display"},
			},
			Children: []NodeID{5, 8},
		},
		{ // 5: span.item-count — count expr + " items" text
			Kind: NodeElement,
			Tag:  "span",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "item-count"},
			},
			Children: []NodeID{6, 7},
		},
		{ // 6: expr node — count display
			Kind: NodeExpr,
			Expr: ExprID(4),
		},
		{ // 7: text " items"
			Kind: NodeText,
			Text: " items",
		},
		{ // 8: pre.item-list
			Kind: NodeElement,
			Tag:  "pre",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "item-list"},
			},
			Children: []NodeID{9},
		},
		{ // 9: expr node — items display
			Kind: NodeExpr,
			Expr: ExprID(3),
		},
		{ // 10: div.list-actions
			Kind: NodeElement,
			Tag:  "div",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "class", Value: "list-actions"},
			},
			Children: []NodeID{11, 12},
		},
		{ // 11: button "Remove Last"
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrEvent, Name: "click", Event: "removeLastItem"},
			},
			Children: []NodeID{14},
		},
		{ // 12: button "Clear All"
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrEvent, Name: "click", Event: "clearItems"},
			},
			Children: []NodeID{15},
		},
		{ // 13: text "Add"
			Kind: NodeText,
			Text: "Add",
		},
		{ // 14: text "Remove Last"
			Kind: NodeText,
			Text: "Remove Last",
		},
		{ // 15: text "Clear All"
			Kind: NodeText,
			Text: "Clear All",
		},
	}

	return &Program{
		Name:  "List",
		Nodes: nodes,
		Root:  0,
		Exprs: exprs,
		Signals: []SignalDef{
			{Name: "items", Type: TypeString, Init: ExprID(0)},
			{Name: "input", Type: TypeString, Init: ExprID(1)},
			{Name: "count", Type: TypeInt, Init: ExprID(2)},
		},
		Handlers: []Handler{
			{Name: "addItem", Body: []ExprID{10, 14}},
			{Name: "removeLastItem", Body: []ExprID{18}},
			{Name: "clearItems", Body: []ExprID{20, 22}},
		},
		StaticMask: []bool{
			false, // 0: div.list-demo (contains dynamic subtree)
			true,  // 1: div.list-input (static structure)
			true,  // 2: input (static element, value via events)
			true,  // 3: button "Add"
			false, // 4: div.list-display (contains expr)
			false, // 5: span.item-count (contains expr)
			false, // 6: expr node (count)
			true,  // 7: text " items"
			false, // 8: pre.item-list (contains expr)
			false, // 9: expr node (items display)
			true,  // 10: div.list-actions (static structure)
			true,  // 11: button "Remove Last"
			true,  // 12: button "Clear All"
			true,  // 13: text "Add"
			true,  // 14: text "Remove Last"
			true,  // 15: text "Clear All"
		},
	}
}
