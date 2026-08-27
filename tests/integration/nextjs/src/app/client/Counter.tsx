"use client";

import { useState } from "react";

import { echoAction } from "../actions";

export function Counter() {
  const [echo, setEcho] = useState("CLIENT_MARKER");
  return (
    <button
      type="button"
      onClick={() => {
        void echoAction("clicked").then(setEcho);
      }}
    >
      {echo}
    </button>
  );
}
