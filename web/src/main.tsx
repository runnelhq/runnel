import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Root } from "./Root";
import { ErrorBoundary, PageError } from "./components/ErrorBoundary";
import "./index.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ErrorBoundary fallback={(error) => <PageError error={error} />}>
      <Root />
    </ErrorBoundary>
  </StrictMode>,
);
