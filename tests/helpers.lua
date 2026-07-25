--- Shared helpers for the Lua suite.
---
--- Tier 1: every tree is built under a fresh temporary directory and removed
--- again, so nothing here touches the developer's filesystem or the network.

local M = {}

local uv = vim.uv or vim.loop

--- Build a directory tree and return its root.
---
--- A path ending in "/" is a directory; anything else is a file. Only existence
--- is ever checked, so contents are a placeholder.
---@param paths string[]
---@return string root
function M.tree(paths)
  local root = vim.fn.tempname()
  vim.fn.mkdir(root, "p")
  -- macOS puts temporary files under /var, which is a symlink to /private/var.
  -- nvim_buf_set_name resolves it and root.find does not, so the two disagree
  -- unless the fixture is realpath'd up front.
  root = uv.fs_realpath(root) or root
  for _, p in ipairs(paths) do
    local full = vim.fs.joinpath(root, p)
    if p:sub(-1) == "/" then
      vim.fn.mkdir(full, "p")
    else
      vim.fn.mkdir(vim.fs.dirname(full), "p")
      local fd = assert(uv.fs_open(full, "w", 420))
      uv.fs_write(fd, "x")
      uv.fs_close(fd)
    end
  end
  return root
end

--- Remove a tree built by M.tree.
---@param root string
function M.rmtree(root)
  if root and root ~= "" and root ~= "/" then
    vim.fn.delete(root, "rf")
  end
end

--- Run fn with $PATH replaced by an empty directory, then restore it. Used to
--- make binary resolution deterministic regardless of whether the developer has
--- clew installed.
---@param fn fun(empty_dir: string)
function M.with_empty_path(fn)
  local saved = vim.env.PATH
  local empty = M.tree({ "empty/" })
  vim.env.PATH = vim.fs.joinpath(empty, "empty")
  local ok, err = pcall(fn, vim.fs.joinpath(empty, "empty"))
  vim.env.PATH = saved
  M.rmtree(empty)
  if not ok then error(err) end
end

return M
