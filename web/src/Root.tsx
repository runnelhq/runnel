import { useEffect, useState } from "react";
import { fetchProjectStatus, type ProjectStatus } from "./api";
import { App } from "./App";
import { StartPage } from "./components/StartPage";

/** The example workflow of a new project (internal/scaffold), opened after "Create project". */
const exampleWorkflow = "workflows/hello.workflow.yaml";

/**
 * Root shows the start page while the server's folder has no project (R10), else the
 * editor. After "Create project" the editor opens with the example workflow.
 */
export function Root() {
  const [status, setStatus] = useState<ProjectStatus | null>(null);
  const [openFirst, setOpenFirst] = useState<string | undefined>();
  useEffect(() => {
    // If the status cannot be read, the editor shows what fails.
    fetchProjectStatus().then(setStatus, () => setStatus({ exists: true, dir: "", empty: false }));
  }, []);
  if (!status) return null;
  if (!status.exists) {
    return (
      <StartPage
        status={status}
        onCreated={(created) => {
          setOpenFirst(created.includes(exampleWorkflow) ? exampleWorkflow : undefined);
          setStatus({ ...status, exists: true });
        }}
      />
    );
  }
  return <App openFirst={openFirst} />;
}
