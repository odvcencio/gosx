package ui

// InputProps keeps the label, native form contract, and validation state together.
type InputProps struct {
	ID          string
	Name        string
	Type        string
	Label       string
	Placeholder string
	Value       string
	Help        string
	Error       string
	Required    bool
	Disabled    bool
	Invalid     bool
}

// Input renders a native labeled input with linked help and error descriptions.
component Input(props: InputProps) {
	return <div class="gsx-field" data-invalid={props.Invalid}>
		<label class="gsx-field__label" for={props.ID}>{props.Label}</label>
		<input
			class="gsx-input"
			id={props.ID}
			name={props.Name}
			type={props.Type}
			placeholder={props.Placeholder}
			value={props.Value}
			aria-describedby={props.ID + "-help " + props.ID + "-error"}
			aria-invalid={props.Invalid}
			required={props.Required}
			disabled={props.Disabled}
		 />
		<p class="gsx-field__help" id={props.ID + "-help"}>{props.Help}</p>
		<p class="gsx-field__error" id={props.ID + "-error"} role="alert">{props.Error}</p>
	</div>
}
