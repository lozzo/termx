import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./i18n";
import HomePage from "./HomePage";
import LoginPage from "./LoginPage";
import ConsolePage from "./ConsolePage";
import "./styles.css";

document.documentElement.dataset.wxTheme =
  localStorage.getItem("muxvia-wx-theme") === "neutral-dark"
    ? "neutral-dark"
    : "light-gray";
const path = window.location.pathname.replace(/\/$/, "") || "/";
const Page =
  path === "/login"
    ? LoginPage
    : path === "/account" || path === "/operator" || path.startsWith("/operator/")
      ? ConsolePage
      : HomePage;
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Page />
  </StrictMode>,
);
