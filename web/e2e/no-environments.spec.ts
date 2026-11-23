import { expect, test } from "@playwright/test";

// A project without environments, but with variables (like examples/expressions):
// project.yaml shows its reference and the variables instead of an empty page. On the
// server of examples/pipelines (only read); its variables come from the answer below,
// with "environments": null as older servers sent it (the server now sends [], tested in
// internal/server).
test.use({ baseURL: "http://127.0.0.1:7397" });

test("project.yaml of a project without environments shows the reference", async ({ page }) => {
  await page.route("**/api/v1/variables", (route) =>
    route.fulfill({
      json: {
        environments: null,
        variables: [
          {
            name: "THRESHOLD",
            project: { value: "10", file: "project.yaml", line: 7 },
            environments: {},
            uses: [{ file: "workflows/import.workflow.yaml", line: 19, column: 12 }],
          },
        ],
      },
    }),
  );
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto("/");
  await page.getByRole("tree").getByText("project.yaml", { exact: true }).click();
  await expect(page.getByTestId("settings-form")).toBeVisible();
  const ref = page.getByRole("region", { name: "Reference" });
  await expect(ref.getByTestId("field-name")).toBeVisible();
  await ref.getByRole("button", { name: "Variables", exact: true }).click();
  const vars = page.getByRole("region", { name: "Variables" });
  await expect(vars.getByTestId("variable-THRESHOLD")).toContainText("1 use");
  // Without environments nothing is missing: no warning in the file bar.
  await expect(page.getByTestId("variable-warning")).toHaveCount(0);
  expect(errors).toEqual([]);
});
