// Use the same PocketBase control as the administrator, including its popover.
export async function selectChoice(control, label) {
    await control.click();
    await control.locator('..').getByRole('button', { name: label, exact: true }).click();
}
