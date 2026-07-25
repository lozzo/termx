import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./i18n";
import HomePage from "./HomePage";
import LoginPage from "./LoginPage";
import AccountPage from "./AccountPage";
import OperatorPage from "./OperatorPage";
import "./styles.css";

document.documentElement.dataset.wxTheme =
  localStorage.getItem("muxvia-wx-theme") === "neutral-dark"
    ? "neutral-dark"
    : "light-gray";
const path = window.location.pathname.replace(/\/$/, "") || "/";
const Page =
  path === "/login"
    ? LoginPage
    : path === "/account"
      ? AccountPage
      : path === "/operator" || path.startsWith("/operator/")
        ? OperatorPage
        : HomePage;
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Page />
  </StrictMode>,
);
