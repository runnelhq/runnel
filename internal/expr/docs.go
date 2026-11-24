package expr

// Reference of the names and functions of workflow expressions, for the editor's
// suggestions and its reference (roadmap S35, mockup design/mockups/s35-expressions.html).
// The examples are compiled by the tests, so that the reference stays correct.

// Doc describes a name of the context or a function.
type Doc struct {
	Name string `json:"name"`
	// Signature of a function, e.g. "formatDate(date, pattern)"; empty for names.
	Signature string `json:"signature,omitempty"`
	// Type of the value or the result: string, integer, number, bool, date, list, map, any.
	Type        string `json:"type"`
	Description string `json:"description"`
	Example     string `json:"example,omitempty"`
	// Group of a function in the reference, e.g. "Date".
	Group string `json:"group,omitempty"`
	// Fields of a name, e.g. the fields of run.
	Fields []Doc `json:"fields,omitempty"`
}

// resultFields are the fields of result and of each entry of results.
var resultFields = []Doc{
	{Name: "success", Type: "bool", Description: "Whether the action succeeded"},
	{Name: "status", Type: "string", Description: "succeeded, failed, skipped or cancelled"},
	{Name: "output", Type: "any", Description: "Output data of the action, e.g. the list of files of file.list"},
	{Name: "error", Type: "string", Description: "Reason of the failure; empty if it succeeded"},
	{Name: "durationMs", Type: "integer", Description: "Duration in milliseconds"},
	{Name: "id", Type: "string", Description: "Id of the action"},
	{Name: "type", Type: "string", Description: "Type of the action, e.g. core.log"},
}

// Roots describes the names an expression in a workflow can use (RowRoots: WorkflowRoots
// and row, which only actions with forEach may use).
var Roots = []Doc{
	{Name: "params", Type: "map", Description: "Parameters of the workflow, with their declared type", Example: "{{ params.ORDERS > 10 }}"},
	{Name: "vars", Type: "map", Description: "All variables of the run as text: parameters, project, environment", Example: "{{ vars.THRESHOLD }}"},
	{Name: "env", Type: "map", Description: "Environment variables of the operating system", Example: "{{ env.COMPUTERNAME }}"},
	{Name: "run", Type: "map", Description: "The current run", Fields: []Doc{
		{Name: "id", Type: "string", Description: "Id of the run"},
		{Name: "workflow", Type: "string", Description: "Name of the workflow"},
		{Name: "environment", Type: "string", Description: "Name of the environment; empty without one"},
		{Name: "startedAt", Type: "date", Description: "Start of the run"},
		{Name: "lastError", Type: "string", Description: "Error of the last failed action; empty if none failed"},
	}, Example: "{{ run.lastError }}"},
	{Name: "result", Type: "map", Description: "Result of the action the link starts from (in link conditions)", Fields: resultFields, Example: "{{ result.success }}"},
	{Name: "results", Type: "map", Description: "Results of the actions that have run, by action id", Fields: resultFields, Example: "{{ results.load.status }}"},
	{Name: "row", Type: "any", Description: "The current entry of forEach; only in actions with forEach", Example: "{{ row.path }}"},
	{Name: "request", Type: "map", Description: "The request of the caller of an http trigger; nil if the run was not started by one", Fields: []Doc{
		{Name: "method", Type: "string", Description: "HTTP method, e.g. POST"},
		{Name: "path", Type: "string", Description: "Path of the request, e.g. /api/customers/4711"},
		{Name: "headers", Type: "map", Description: "Headers by name, e.g. request.headers['Content-Type']"},
		{Name: "query", Type: "map", Description: "Query parameters by name (the first value of each)"},
		{Name: "body", Type: "any", Description: "The body: the JSON value, the fields of a form, or the text"},
		{Name: "client", Type: "string", Description: "Address of the caller"},
	}, Example: "{{ request?.body?.customer?.id }}"},
}

// Functions describes the functions of expressions: the library of Runnel (ADR 0006)
// and the most useful built-in functions of expr-lang.
var Functions = []Doc{
	{Group: "Date", Name: "today", Signature: "today()", Type: "date", Description: "Today, 00:00 local time", Example: "{{ today() }}"},
	{Group: "Date", Name: "now", Signature: "now()", Type: "date", Description: "The current date and time", Example: "{{ now() }}"},
	{Group: "Date", Name: "formatDate", Signature: "formatDate(date, pattern)", Type: "string",
		Description: "Writes a date as text. Pattern letters: yyyy year, MM month, dd day, HH:mm:ss time, EEE weekday, MMMM month name, 'text' in quotes",
		Example:     "{{ formatDate(today(), 'dd.MM.yyyy') }}"},
	{Group: "Date", Name: "parseDate", Signature: "parseDate(text, pattern)", Type: "date", Description: "Reads a date from text with a pattern like formatDate", Example: "{{ parseDate('19.11.2026', 'dd.MM.yyyy') }}"},
	{Group: "Date", Name: "addDays", Signature: "addDays(date, n)", Type: "date", Description: "Adds days; negative numbers go back", Example: "{{ addDays(today(), -1) }}"},
	{Group: "Date", Name: "addHours", Signature: "addHours(date, n)", Type: "date", Description: "Adds hours", Example: "{{ addHours(now(), 2) }}"},
	{Group: "Date", Name: "addMonths", Signature: "addMonths(date, n)", Type: "date", Description: "Adds months", Example: "{{ addMonths(today(), 1) }}"},
	{Group: "Date", Name: "dayOfWeek", Signature: "dayOfWeek(date)", Type: "integer", Description: "Weekday: 1 Monday … 7 Sunday", Example: "{{ dayOfWeek(today()) >= 6 }}"},
	{Group: "Date", Name: "diffDays", Signature: "diffDays(from, to)", Type: "integer", Description: "Days from one date to another", Example: "{{ diffDays('2026-11-19', today()) }}"},

	{Group: "Text", Name: "upper", Signature: "upper(text)", Type: "string", Description: "Text in capital letters", Example: "{{ upper('abc') }}"},
	{Group: "Text", Name: "lower", Signature: "lower(text)", Type: "string", Description: "Text in small letters", Example: "{{ lower('ABC') }}"},
	{Group: "Text", Name: "trim", Signature: "trim(text)", Type: "string", Description: "Removes spaces at the start and end", Example: "{{ trim(' a ') }}"},
	{Group: "Text", Name: "replace", Signature: "replace(text, old, new)", Type: "string", Description: "Replaces all occurrences", Example: "{{ replace('a-b', '-', '_') }}"},
	{Group: "Text", Name: "replaceRegex", Signature: "replaceRegex(text, pattern, new)", Type: "string", Description: "Replaces every match of a regular expression (write patterns with \\ in backticks, e.g. `\\d+`); new may use groups as $1 or ${name}, $$ for a $", Example: "{{ replaceRegex('0221 / 123-45', '[^0-9]', '') }}"},
	{Group: "Text", Name: "split", Signature: "split(text, separator)", Type: "list", Description: "Splits text into a list", Example: "{{ split('a;b', ';') }}"},
	{Group: "Text", Name: "padLeft", Signature: "padLeft(text, length, fill)", Type: "string", Description: "Fills on the left up to a length, e.g. 007; fill defaults to a space", Example: "{{ padLeft('7', 3, '0') }}"},
	{Group: "Text", Name: "padRight", Signature: "padRight(text, length, fill)", Type: "string", Description: "Fills on the right up to a length", Example: "{{ padRight('ab', 4) }}"},
	{Group: "Text", Name: "substr", Signature: "substr(text, start, length)", Type: "string", Description: "Part of a text, counting from 0; length is optional", Example: "{{ substr('Runnel', 1, 3) }}"},
	{Group: "Text", Name: "left", Signature: "left(text, n)", Type: "string", Description: "The first n characters", Example: "{{ left('Runnel', 3) }}"},
	{Group: "Text", Name: "right", Signature: "right(text, n)", Type: "string", Description: "The last n characters", Example: "{{ right('Runnel', 3) }}"},
	{Group: "Text", Name: "concat", Signature: "concat(separator, a, b, …)", Type: "string",
		Description: "Joins values with a separator and leaves out empty ones, so no separator stands alone; lists count with their entries",
		Example:     "{{ concat(', ', record.street, record.city) }}"},
	{Group: "Text", Name: "normalize", Signature: "normalize(text)", Type: "string", Description: "Replaces umlauts and ß, joins spaces – e.g. for file names", Example: "{{ normalize('Jürgen Groß') }}"},

	{Group: "Paths", Name: "basename", Signature: "basename(path)", Type: "string", Description: "Last part of a path or address, e.g. the file name; / and \\ both separate", Example: "{{ basename('Exports/2026/orders.zip') }}"},
	{Group: "Paths", Name: "dirname", Signature: "dirname(path)", Type: "string", Description: "The path without its last part, e.g. the folder of a file; empty without a folder", Example: "{{ dirname('Exports/2026/orders.zip') }}"},
	{Group: "Paths", Name: "ext", Signature: "ext(path)", Type: "string", Description: "Ending of the file name with its dot, e.g. .zip; empty without one", Example: "{{ ext('Exports/2026/orders.zip') }}"},

	{Group: "Numbers and lists", Name: "len", Signature: "len(value)", Type: "integer", Description: "Number of characters, entries of a list or keys of a map", Example: "{{ len(['a', 'b']) }}"},
	{Group: "Numbers and lists", Name: "round", Signature: "round(number)", Type: "number", Description: "Rounds to a whole number", Example: "{{ round(2.5) }}"},
	{Group: "Numbers and lists", Name: "sum", Signature: "sum(list)", Type: "number", Description: "Sum of a list of numbers", Example: "{{ sum([1, 2, 3]) }}"},
	{Group: "Numbers and lists", Name: "map", Signature: "map(list, .field)", Type: "list", Description: "Takes a field of each entry, e.g. the sizes of files", Example: "{{ map([{'size': 1}], .size) }}"},
	{Group: "Numbers and lists", Name: "filter", Signature: "filter(list, condition)", Type: "list", Description: "Keeps the entries for which the condition is true", Example: "{{ filter([1, 5, 9], # > 3) }}"},
	{Group: "Numbers and lists", Name: "join", Signature: "join(list, separator)", Type: "string", Description: "Joins a list of texts", Example: "{{ join(['a', 'b'], ', ') }}"},

	{Group: "Empty values", Name: "coalesce", Signature: "coalesce(a, b, …)", Type: "any", Description: "The first value that is not empty", Example: "{{ coalesce(run.environment, 'none') }}"},
	{Group: "Empty values", Name: "isEmpty", Signature: "isEmpty(value)", Type: "bool", Description: "Whether a value is empty: nothing, empty text, list or map", Example: "{{ isEmpty(run.lastError) }}"},
}
