import { describe, expect, it } from "vitest";
import {
	cancelQueryArguments,
	isExternalUrl,
	isJobVirtualDocumentUri,
	jobHistoryQuickPickItems,
} from "./commandResultHandler";

describe("isExternalUrl", () => {
	it("returns true for an http url", () => {
		expect(isExternalUrl("http://example.com/sheet")).toBe(true);
	});

	it("returns true for an https url", () => {
		expect(isExternalUrl("https://docs.google.com/spreadsheets/d/abc")).toBe(
			true,
		);
	});

	it("returns false for a local file path", () => {
		expect(isExternalUrl("/Users/foo/Downloads/1.csv")).toBe(false);
	});
});

describe("jobHistoryQuickPickItems", () => {
	it("maps jobs to quick pick items keyed by job uri", () => {
		const items = jobHistoryQuickPickItems({
			jobs: [
				{
					textDocument: { uri: "bqls://project/job1" },
					id: "job1",
					owner: "alice@example.com",
					summary: "SELECT 1",
				},
				{
					textDocument: { uri: "bqls://project/job2" },
					id: "job2",
					owner: "bob@example.com",
					summary: "SELECT 2",
				},
			],
		});

		expect(items).toEqual([
			{
				label: "SELECT 1",
				description: "alice@example.com",
				uri: "bqls://project/job1",
			},
			{
				label: "SELECT 2",
				description: "bob@example.com",
				uri: "bqls://project/job2",
			},
		]);
	});

	it("returns an empty array when there are no jobs", () => {
		expect(jobHistoryQuickPickItems({ jobs: [] })).toEqual([]);
	});
});

describe("isJobVirtualDocumentUri", () => {
	it("returns true for a job virtual document uri", () => {
		expect(
			isJobVirtualDocumentUri(
				"bqls://project/my-project/job/my-job/location/US",
			),
		).toBe(true);
	});

	it("returns false for a table virtual document uri", () => {
		expect(
			isJobVirtualDocumentUri(
				"bqls://project/my-project/dataset/my-dataset/table/my-table",
			),
		).toBe(false);
	});

	it("returns false for a non-bqls uri", () => {
		expect(isJobVirtualDocumentUri("file:///tmp/query.sql")).toBe(false);
	});
});

describe("cancelQueryArguments", () => {
	it("wraps the job uri in an arguments array", () => {
		expect(
			cancelQueryArguments("bqls://project/my-project/job/my-job/location/US"),
		).toEqual(["bqls://project/my-project/job/my-job/location/US"]);
	});
});
