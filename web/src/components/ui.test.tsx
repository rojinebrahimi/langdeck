import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { JsonView, Modal, StatusBadge } from "./ui";
import { RunsTable } from "./RunsTable";
import type { Run } from "../api/types";

describe("ui components", () => {
  it("renders status badges", () => {
    render(<StatusBadge status="failed" />);
    expect(screen.getByText("failed")).toBeInTheDocument();
  });

  it("renders JSON and a dash for empty values", () => {
    const { rerender } = render(<JsonView value={{ a: 1 }} />);
    expect(screen.getByText(/"a": 1/)).toBeInTheDocument();
    rerender(<JsonView value={null} />);
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("closes the modal with Escape", async () => {
    const onClose = vi.fn();
    render(<Modal open title="Hello" onClose={onClose}>body</Modal>);
    expect(screen.getByRole("dialog", { name: "Hello" })).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalled();
  });

  it("lists runs with links", () => {
    const run: Run = {
      id: "run-1", agent_id: "a", agent_name: "support-bot", status: "succeeded", trigger: "ui",
      prompt_tokens: 1000, completion_tokens: 500, created_at: new Date().toISOString(),
    };
    render(<MemoryRouter><RunsTable runs={[run]} /></MemoryRouter>);
    expect(screen.getByText("support-bot")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "run-1" })).toHaveAttribute("href", "/runs/run-1");
    expect(screen.getByText("1.5K")).toBeInTheDocument();
  });

  it("shows an empty state without runs", () => {
    render(<MemoryRouter><RunsTable runs={[]} /></MemoryRouter>);
    expect(screen.getByText("No runs match")).toBeInTheDocument();
  });
});
