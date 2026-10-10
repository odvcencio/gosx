# Typed DOM templates

Author repeatable structure in a server-rendered GoSX component:

```gsx
<template id="item-row-template">
    <li class="item-row">
        <span data-label=""></span>
        <span data-count=""></span>
    </li>
</template>
<ul id="item-list"></ul>
```

Create a row on a keyed list insertion, then keep its child handles for updates:

```go
doc := browser.CurrentDocument()
row := doc.InstantiateTemplate("item-row-template")
if !row.Valid() {
    return
}
label, count := row.Query("[data-label]"), row.Query("[data-count]")
label.SetText(item.Name)
count.SetText(strconv.Itoa(item.Count))
row.SetAttr("data-key", item.Key)
doc.ByID("item-list").Append(row)

// Retain row, label and count with the item's key; later updates reuse them.
count.SetText(strconv.Itoa(updated.Count))
```

`Document.InstantiateTemplate(id string) Element` deeply imports the template's
single element root into the receiving document. The result is detached, and
changing it leaves the template and other instances untouched. Missing IDs,
non-HTML templates, and templates with zero or multiple element roots return
an invalid element. Native builds also return an invalid element.

Top-level text (including indentation) and comments are ignored. Text and
comments inside the root are preserved. Keep all rendered content within that
one root. Use classes or data attributes for repeated descendants; a deep copy
preserves any authored IDs too.

Templates are trusted authored DOM, not a sanitization boundary. Instantiation
does not parse HTML strings or execute scripts. Insertion and browser events
follow ordinary DOM behavior, so author templates without scripts or inline
event handlers. Use `SetText` for arbitrary strings and typed event listeners
for interactions. Cloning runs only when structure is added; frame updates can
reuse handles and skip unchanged values.
