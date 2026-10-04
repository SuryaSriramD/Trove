package workspace

// Syntax is bounded at generation time; semantic validation, reference lookup,
// live guards, capability checks and approval remain the kernel's responsibility.
func actionPlanGrammar() string {
	return `root ::= "{" ws "\"version\"" ws ":" ws "1" ws "," ws "\"actions\"" ws ":" ws "[" ws action (ws "," ws action){0,31} ws "]" ws "}"
action ::= create-page | update-page | create-task | update-task | create-database | create-record | update-record
create-page ::= "{" ws "\"kind\"" ws ":" ws "\"create_page\"" destination title body? ws "}"
create-record ::= "{" ws "\"kind\"" ws ":" ws "\"create_record\"" destination title body? ws "}"
create-task ::= "{" ws "\"kind\"" ws ":" ws "\"create_task\"" destination title body? status? due? ws "}"
update-page ::= "{" ws "\"kind\"" ws ":" ws "\"update_page\"" target (title body? | body) ws "}"
update-task ::= "{" ws "\"kind\"" ws ":" ws "\"update_task\"" target (status due? | due) ws "}"
create-database ::= "{" ws "\"kind\"" ws ":" ws "\"create_database\"" destination title ws "," ws "\"properties\"" ws ":" ws "[" ws (property (ws "," ws property){0,31})? ws "]" body? ws "}"
update-record ::= "{" ws "\"kind\"" ws ":" ws "\"update_record\"" target ws "," ws "\"property\"" ws ":" ws name ws "," ws "\"value\"" ws ":" ws scalar ws "}"
property ::= "{" ws "\"name\"" ws ":" ws name ws "," ws "\"type\"" ws ":" ws ("\"text\"" | "\"number\"" | "\"checkbox\"" | "\"date\"" | "\"url\"" | "\"email\"" | "\"phone\"") ws "}"
destination ::= ws "," ws "\"destination\"" ws ":" ws name
target ::= ws "," ws "\"target\"" ws ":" ws name
title ::= ws "," ws "\"title\"" ws ":" ws name
body ::= ws "," ws "\"body\"" ws ":" ws text
status ::= ws "," ws "\"status\"" ws ":" ws name
due ::= ws "," ws "\"due\"" ws ":" ws "\"" [0-9]{4} "-" [0-9]{2} "-" [0-9]{2} "\""
scalar ::= text | "true" | "false"
name ::= "\"" char{1,512} "\""
text ::= "\"" char{0,4000} "\""
char ::= [^"\\\x7F\x00-\x1F] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F]{4})
ws ::= [ \t\n\r]*
`
}
