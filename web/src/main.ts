import "urlpattern-polyfill";
// Before any component: the theme stylesheet joins every component's styles.
import { syncTheme } from "./theme-runtime";
import { currentProject, currentUser } from "./state";
import "./components/app";

syncTheme(() => ({ user: currentUser.get()?.id ?? "", project: currentProject.get()?.slug ?? "" }));
