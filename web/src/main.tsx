import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import App from "./App";
import "./index.css";

// gcTime outlives a typical viewing session: a list page is unmounted while
// the player is open, and at the 5-minute default its loaded pages were dropped,
// so coming back reloaded page 1 only and the scroll position had nowhere to go.
const qc = new QueryClient({
  defaultOptions: { queries: { staleTime: 30_000, gcTime: 60 * 60_000, retry: 1 } },
});

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={qc}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>
);
