# Activity declarations

Activity records use one finite kind declared before App.Build. Each kind has
up to two semantic dimensions, with at most 16 values each and 64 combinations.
Omitting combinations expands the Cartesian product only when it fits the cap.
Empty dimension slots use `none`; undeclared combinations use one `other,other`
tuple. Dimension values are reviewed nonpersonal enums, at most 64 UTF-8 bytes.

All kinds share fixed `kind`, `dim0` and `dim1` metric label names. Semantic
names belong to dimension-info metrics and administrative records. Each kind
reserves its complete start/finish, participant, record and duration families
before routes can consume capacity. Failed registration reserves no partial
kind, tuple or codec. Build seals declarations.

Activity, participant and event codecs are copied at registration. Each codec
must declare every field and enum; encoders run synchronously against eight
shared bounded staging leases. Avoid capturing mutable game or request state
in an encoder closure. The framework accounts for descriptor memory, while
application-owned closure state belongs to the application's own budget.

Use `telemetry.NewActivityKind` before `App.Build` to register the declaration.
The activity guide describes Begin, participants, events and memory receipts.
There is no implicit file or logger sink.

Hub attachments, route lookup tables, activity descriptors and staging leases
share one 2 MiB miscellaneous reservation. Concurrent admissions cannot each
spend the same capacity. The default inventory test uses these production
owners, reserving hub, loop and activity families before routes.
