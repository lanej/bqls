export interface JobHistory {
	textDocument: { uri: string };
	id: string;
	owner: string;
	summary: string;
}

export interface ListJobHistoryResult {
	jobs: JobHistory[];
}

export interface JobHistoryQuickPickItem {
	label: string;
	description: string;
	uri: string;
}

export function isExternalUrl(url: string): boolean {
	return url.startsWith("http://") || url.startsWith("https://");
}

// Mirrors the server's VirtualTextDocumentInfo.validate (document_uri.go):
// only job virtual documents (as opposed to table ones) can be cancelled via
// bqls.cancelQuery.
export function isJobVirtualDocumentUri(uriString: string): boolean {
	return /\/job\/[^/]+\/location\/[^/]+/.test(uriString);
}

export function cancelQueryArguments(jobUri: string): unknown[] {
	return [jobUri];
}

export function jobHistoryQuickPickItems(
	result: ListJobHistoryResult,
): JobHistoryQuickPickItem[] {
	return result.jobs.map((job) => ({
		label: job.summary,
		description: job.owner,
		uri: job.textDocument.uri,
	}));
}
