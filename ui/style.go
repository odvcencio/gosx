package ui

const stylesheet = `
.gosx-ui{box-sizing:border-box}
.gosx-ui-box{min-width:0}
.gosx-ui-stack{display:flex;flex-direction:column}
.gosx-ui-inline{display:flex;flex-wrap:wrap}
.gosx-ui-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(16rem,100%),1fr))}
.gosx-ui-gap-xs{gap:.375rem}.gosx-ui-gap-sm{gap:.5rem}.gosx-ui-gap-md{gap:.75rem}.gosx-ui-gap-lg{gap:1rem}.gosx-ui-gap-xl{gap:1.5rem}
.gosx-ui-align-start{align-items:flex-start}.gosx-ui-align-center{align-items:center}.gosx-ui-align-end{align-items:flex-end}.gosx-ui-align-stretch{align-items:stretch}
.gosx-ui-justify-start{justify-content:flex-start}.gosx-ui-justify-center{justify-content:center}.gosx-ui-justify-end{justify-content:flex-end}.gosx-ui-justify-between{justify-content:space-between}
.gosx-ui-text{margin:0;color:inherit;line-height:1.55}
.gosx-ui-text-sm{font-size:.875rem}.gosx-ui-text-md{font-size:1rem}.gosx-ui-text-lg{font-size:1.125rem}.gosx-ui-text-xl{font-size:1.35rem}
.gosx-ui-weight-medium{font-weight:500}.gosx-ui-weight-semibold{font-weight:600}.gosx-ui-weight-bold{font-weight:700}
.gosx-ui-tone-muted{color:color-mix(in srgb,currentColor 62%,transparent)}
.gosx-ui-tone-danger{color:#b42318}.gosx-ui-tone-success{color:#067647}.gosx-ui-tone-warning{color:#b54708}
.gosx-ui-button{appearance:none;border:1px solid transparent;border-radius:.5rem;display:inline-flex;align-items:center;justify-content:center;gap:.5rem;font:inherit;font-weight:600;text-decoration:none;white-space:nowrap;cursor:pointer;transition:background-color .15s,border-color .15s,color .15s,box-shadow .15s}
.gosx-ui-button:focus-visible,.gosx-ui-input:focus-visible,.gosx-ui-textarea:focus-visible,.gosx-ui-select:focus-visible,.gosx-ui-tab:focus-visible{outline:2px solid #2563eb;outline-offset:2px}
.gosx-ui-button[disabled],.gosx-ui-button[aria-disabled=true]{opacity:.55;cursor:not-allowed}
.gosx-ui-button-sm{min-height:2rem;padding:.25rem .625rem;font-size:.875rem}.gosx-ui-button-md{min-height:2.5rem;padding:.5rem .875rem}.gosx-ui-button-lg{min-height:3rem;padding:.625rem 1.125rem;font-size:1.05rem}
.gosx-ui-button-default{background:#111827;color:#fff;border-color:#111827}.gosx-ui-button-default:hover{background:#1f2937}
.gosx-ui-button-secondary{background:#f3f4f6;color:#111827;border-color:#d1d5db}.gosx-ui-button-secondary:hover{background:#e5e7eb}
.gosx-ui-button-ghost{background:transparent;color:#111827}.gosx-ui-button-ghost:hover{background:#f3f4f6}
.gosx-ui-button-danger{background:#b42318;color:#fff;border-color:#b42318}.gosx-ui-button-danger:hover{background:#912018}
.gosx-ui-card{border:1px solid #e5e7eb;border-radius:.5rem;background:#fff;color:#111827;box-shadow:0 1px 2px rgb(16 24 40 / .05)}
.gosx-ui-card-header,.gosx-ui-card-content,.gosx-ui-card-footer{padding:1rem}
.gosx-ui-card-header{border-bottom:1px solid #f3f4f6}.gosx-ui-card-footer{border-top:1px solid #f3f4f6}
.gosx-ui-card-title{margin:0;color:#111827}
.gosx-ui-badge{display:inline-flex;align-items:center;border:1px solid transparent;border-radius:999px;padding:.125rem .5rem;font-size:.75rem;font-weight:600;line-height:1.4}
.gosx-ui-badge-default{background:#eef2ff;color:#3730a3;border-color:#c7d2fe}.gosx-ui-badge-success{background:#ecfdf3;color:#067647;border-color:#abefc6}.gosx-ui-badge-warning{background:#fffaeb;color:#b54708;border-color:#fedf89}.gosx-ui-badge-danger{background:#fef3f2;color:#b42318;border-color:#fecdca}
.gosx-ui-field-label{font-size:.875rem;font-weight:600;color:#111827}
.gosx-ui-field-help{margin:0;color:#6b7280;font-size:.875rem}.gosx-ui-field-error{margin:0;color:#b42318;font-size:.875rem}
.gosx-ui-input,.gosx-ui-textarea,.gosx-ui-select{width:100%;border:1px solid #d1d5db;border-radius:.5rem;background:#fff;color:#111827;font:inherit;padding:.55rem .75rem;min-height:2.5rem}
.gosx-ui-textarea{min-height:5rem;resize:vertical}.gosx-ui-input::placeholder,.gosx-ui-textarea::placeholder{color:#9ca3af}
.gosx-ui-input[disabled],.gosx-ui-textarea[disabled],.gosx-ui-select[disabled]{background:#f9fafb;color:#6b7280;cursor:not-allowed}
.gosx-ui-checkbox{display:inline-flex;align-items:center;gap:.5rem;font-size:.95rem;color:#111827}.gosx-ui-checkbox-input{width:1rem;height:1rem;accent-color:#111827}
.gosx-ui-tabs{display:flex;gap:.25rem;border-bottom:1px solid #e5e7eb}
.gosx-ui-tab{appearance:none;border:0;border-bottom:2px solid transparent;background:transparent;color:#6b7280;font:inherit;font-weight:600;text-decoration:none;padding:.625rem .75rem;cursor:pointer}
.gosx-ui-tab.is-active{border-bottom-color:#111827;color:#111827}
.gosx-ui-table{width:100%;border-collapse:collapse;font-size:.95rem}.gosx-ui-table th,.gosx-ui-table td{border-bottom:1px solid #e5e7eb;text-align:left;padding:.75rem}.gosx-ui-table th{font-weight:700;color:#374151;background:#f9fafb}
`
