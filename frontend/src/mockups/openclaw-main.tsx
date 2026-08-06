import React from "react";
import { createRoot } from "react-dom/client";

import "@/i18n";
import "@/styles/globals.css";
import { OpenClawIntegrationMockup } from "./openclaw-integration";

const container = document.getElementById("root");

if (!container) {
  throw new Error("root element not found");
}

createRoot(container).render(
  <React.StrictMode>
    <OpenClawIntegrationMockup />
  </React.StrictMode>,
);
