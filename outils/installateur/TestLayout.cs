using System;
using System.Drawing;
using System.Globalization;
using System.IO;
using System.Reflection;
using System.Threading;
using System.Windows.Forms;

// Loads the actual packaged UI without ever invoking its installation handler.
internal static class TestLayout
{
    private static void Require(bool condition, string message)
    {
        if (!condition) throw new InvalidOperationException(message);
    }

    private static void CheckText(Control parent)
    {
        foreach (Control control in parent.Controls)
        {
            if (!control.Visible) continue;
            if (control is Label || control is CheckBox || control is Button)
            {
                Size required = control.GetPreferredSize(new Size(control.Width, 0));
                Require(control.Height >= required.Height,
                    "Clipped text: " + control.Text + " (" + control.Height + " < " + required.Height + ")");
            }
            CheckText(control);
        }
    }

    private static TextBox FindPath(Control parent)
    {
        foreach (Control control in parent.Controls)
        {
            TextBox path = control as TextBox;
            if (path != null) return path;
            path = FindPath(control);
            if (path != null) return path;
        }
        return null;
    }

    [STAThread]
    private static int Main(string[] args)
    {
        try
        {
            Thread.CurrentThread.CurrentUICulture = CultureInfo.GetCultureInfo(args[1]);
            Application.EnableVisualStyles();
            Application.SetCompatibleTextRenderingDefault(false);
            Assembly setup = Assembly.LoadFrom(Path.GetFullPath(args[0]));
            Type windowType = setup.GetType("SetupWindow", true);
            Directory.CreateDirectory(args[2]);
            int cases = 0;
            foreach (int width in new int[] { 550, 650, 900 })
            foreach (int length in new int[] { 0, 260 })
            foreach (int fontSize in new int[] { 10, 14 })
            {
                using (Form window = (Form)Activator.CreateInstance(windowType, true))
                {
                    window.ShowInTaskbar = false;
                    window.Opacity = 0;
                    window.Show();
                    float scale;
                    using (Graphics graphics = window.CreateGraphics()) scale = graphics.DpiX / 96F;
                    window.ClientSize = new Size((int)(width * scale), (int)(480 * scale));
                    window.Font = new Font("Segoe UI", fontSize);
                    TextBox path = FindPath(window);
                    Require(path != null, "Installation path is missing");
                    if (length > 0)
                    {
                        const string prefix = @"C:\Users\";
                        const string suffix = @"\AppData\Local\Programs\ForeverPulseCompanion";
                        path.Text = prefix + new string('W', length - prefix.Length - suffix.Length) + suffix;
                    }
                    path.Select(0, 0);
                    window.PerformLayout();
                    Application.DoEvents();
                    CheckText(window);
                    Require(path.ReadOnly && path.Multiline && path.WordWrap && path.TabStop,
                        "Installation path must wrap and remain selectable/read-only");
                    Point last = path.GetPositionFromCharIndex(path.TextLength - 1);
                    Require(last.Y + path.Font.Height <= path.ClientSize.Height, "Last path line is clipped");
                    Require(last.X >= 0 && last.X < path.ClientSize.Width, "Path is clipped horizontally");
                    TableLayoutPanel body = (TableLayoutPanel)path.Parent;
                    int bottom = body.Padding.Top;
                    foreach (Control control in body.Controls)
                    {
                        if (!control.Visible) continue;
                        Require(control.Top >= bottom, "Overlapping body rows: " + control.Text);
                        Require(control.Right <= body.ClientSize.Width - body.Padding.Right, "Body overflows horizontally");
                        bottom = control.Bottom;
                    }
                    foreach (string field in new string[] { "install", "close" })
                    {
                        Control button = (Control)windowType.GetField(field, BindingFlags.Instance | BindingFlags.NonPublic).GetValue(window);
                        Rectangle bounds = window.RectangleToClient(button.RectangleToScreen(button.ClientRectangle));
                        Require(window.ClientRectangle.Contains(bounds), "Action button is outside the window");
                    }
                    // Status text changes must resize its row rather than overlap the next one.
                    Label status = (Label)windowType.GetField("status", BindingFlags.Instance | BindingFlags.NonPublic).GetValue(window);
                    status.Text = args[1] == "fr-FR"
                        ? "Installation terminée. Cliquez sur Ouvrir pour lancer la version 0.10.0-rc.6, ou sur Fermer pour la lancer plus tard."
                        : "Setup complete. Click Open to start version 0.10.0-rc.6, or Close to start it later.";
                    window.PerformLayout();
                    Application.DoEvents();
                    CheckText(window);
                    Panel viewport = (Panel)body.Parent;
                    viewport.ScrollControlIntoView(status);
                    Rectangle statusBounds = viewport.RectangleToClient(status.RectangleToScreen(status.ClientRectangle));
                    Require(viewport.ClientRectangle.Contains(statusBounds), "Status cannot be reached by scrolling");
                    viewport.AutoScrollPosition = Point.Empty;
                    using (Bitmap image = new Bitmap(window.Width, window.Height))
                    {
                        window.DrawToBitmap(image, new Rectangle(Point.Empty, image.Size));
                        image.Save(Path.Combine(args[2], args[1] + "-" + width + "-path" + length + "-font" + fontSize + ".png"));
                    }
                    cases++;
                }
            }
            File.WriteAllText(Path.Combine(args[2], args[1] + ".txt"), "PASS: " + cases + " native layout cases; no installation executed.");
            return 0;
        }
        catch (Exception error)
        {
            File.WriteAllText(Path.Combine(args[2], args[1] + "-error.txt"), error.ToString());
            return 1;
        }
    }
}
