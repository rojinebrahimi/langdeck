import "@testing-library/jest-dom/vitest";
import { configure } from "@testing-library/react";

// findBy*/waitFor default to 1 s. The first test in a file also pays for module
// loading, which can exceed that on a busy CI runner; 3 s removes the flake
// without slowing passing tests.
configure({ asyncUtilTimeout: 3000 });
